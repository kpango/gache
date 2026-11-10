package gache

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	wordSize    = unsafe.Sizeof(uintptr(0))
	twoWordSize = wordSize * 2
)

// value.state layout: [ generation | stPtr | st ], st being one of the values
// below. The generation grows by stGen on every tombstone.
const (
	stLive     uint64 = 0 // payload and expire are published
	stDeleted  uint64 = 1 // tombstoned; revivable in place under n.mu
	stExpunged uint64 = 2 // dropped from dirty for good; never revived
	stMask     uint64 = 3
	// stPtr marks a word-sized V that holds a pointer (typeHasPointers). Its payload
	// word must be written with a barriered pointer store, a plain uint64 store is
	// invisible to the GC.
	stPtr uint64 = 1 << 2
	stGen uint64 = 1 << 3
)

// shardMap is an unexported concurrent map keyed by a precomputed uint64 hash of
// the K key (gache hashes string keys for shard selection and reuses the hash;
// Map hashes with maphash.Comparable). It is the single storage layer of both
// public types: gache uses one shardMap[string, V] per shard, Map wraps one
// shardMapOf[K, *V, struct{}, struct{}]. It is modeled on the proven sync.Map algorithm
// (read/dirty/misses/promote/amended) but differs from it in these ways:
//
//  1. The map value is a *value[K, V] chain HEAD. value[K, V] nodes are simultaneously
//     the cache entry AND a collision-chain node (linked via next). Collisions on
//     the uint64 hash are resolved by walking the chain and comparing the original
//     key. This avoids a separate entry object: a brand-new key costs ONE
//     node allocation.
//  2. value[K, V] nodes are SHARED between the read snapshot map and the dirty map
//     (same pointers, exactly like sync.Map's shared entries). Nodes are never
//     copied. In-place mutation and tombstoning are therefore visible through both
//     views. Promotion copies the map[uint64]*value[K, V] (head pointers shared)
//     and physically drops fully-tombstoned chains so memory is reclaimed.
//  3. A tombstoned node is revived in place under the node mutex alone, with no
//     map-wide mutex (sync.Map's expunged invariant, below). Swap reads the
//     previous value and writes under one node lock (upsert), Size is
//     allocation-free, and the node carries a plain Mutex, not an RWMutex.
//
// Node state machine. All transitions happen under the node mutex n.mu; readers
// never take it:
//
//	live ---tombstone---> deleted ---revive (n.mu only, no m.mu)---> live
//	                         |
//	                         +--dirtyLocked / dirty-only unlink (m.mu + n.mu)--> expunged
//
// An expunged node is never revived or reused: it has been (or is being)
// dropped from the dirty map, so a writer that finds one falls to the m.mu slow
// path and inserts a fresh node into dirty.
//
// n.mu is also held outside this file: ExtendExpire, GetRefreshWithDur and
// SetWithExpireIfNotExists (gache.go) lock a node they found with loadNode and
// read and write its payload and expire themselves, hand-rolled for performance
// (see ExtendExpire). They follow the same protocol: lock, check live(), then
// re-read expire. The one writer of expire that does not hold n.mu is the
// lock-free same-value overwrite in store: for a two-word V that equals the stored
// one it calls storeExpire without n.mu, so expire of a live node can change under
// an n.mu holder (see tombstoneIfExpired).
//
// Strict invariant (m.mu held): while dirty is non-nil, every non-expunged node
// reachable from the read snapshot is also reachable from dirty, and no expunged
// node is reachable from dirty. Whoever unlinks a node from dirty (dirtyLocked, and
// popSlow and expireSlow through their shared evictSlow) expunges it in the same
// n.mu critical section that observes it dead, before unlinking: a node a fast
// path can still revive is never taken out of dirty.
//
// Lock-free readers use the state word as a seqlock for tombstone and revive
// only. tombstone stores the new state (generation + 1, deleted) BEFORE it clears
// the payload; revive writes the payload and expire BEFORE it stores the live
// state, with the generation the tombstone assigned. A reader loads state,
// payload and expire, loads state again and accepts the result only if both state
// words are equal and live, so it never accepts a payload from another
// incarnation (no live state of an earlier incarnation can recur) or a cleared
// payload. An in-place overwrite of a live node does not touch the state word:
// val and expire are each read atomically, but they are NOT a consistent pair
// across such an overwrite.
//
// Pointer-shaped words stay visible to the GC write barrier: atomic.StoreUint64
// on a pointer is not barriered. Whether a word-sized V is a pointer is decided
// once per node, at creation (reflect, the stPtr bit of the state word), never on
// the read path. A pointer word is written with the barriered atomic.StorePointer
// and read with atomic.LoadPointer, a scalar word with atomic.StoreUint64 and
// atomic.LoadUint64. There is no mirror field.
//
// Length is one atomic.Int64 per shardMap, replaced by clear. A tombstone
// decrements the counter captured from the read snapshot it found the node in, so
// a node from before a clear cannot corrupt the length of the map after it.
//
// Reads are lock-free at the map-structure level (a single atomic load of the
// read snapshot) and at the node level (the seqlock above), for a word-sized V and
// for a V with an installed valPtr. The rare amended-miss path falls back under mu.
type shardMapOf[K comparable, V any, P valPtrSlot[V], E expireSlot] struct {
	read   atomic.Pointer[readOnlyShardOf[K, V, P, E]]
	dirty  map[uint64]*valueOf[K, V, P, E]
	l      atomic.Pointer[atomic.Int64]
	misses int
	// chained is the number of nodes beyond the first of their bucket in the
	// structure that holds every non-expunged node: dirty while it is non-nil,
	// else the read snapshot. A promotion hands dirty over as the read snapshot
	// unchanged, so one counter serves both. Guarded by mu; see size.
	chained int
	mu      sync.Mutex
}

type readOnlyShardOf[K comparable, V any, P valPtrSlot[V], E expireSlot] struct {
	m       map[uint64]*valueOf[K, V, P, E]
	c       *atomic.Int64
	amended bool
}

func (m *shardMapOf[K, V, P, E]) counter() *atomic.Int64 {
	c := m.l.Load()
	if c != nil {
		return c
	}
	c = new(atomic.Int64)
	if m.l.CompareAndSwap(nil, c) {
		return c
	}
	return m.l.Load()
}

// counterFor returns c, the length counter of a read snapshot, or the shard's
// current counter when the snapshot predates any counter.
func (m *shardMapOf[K, V, P, E]) counterFor(c *atomic.Int64) *atomic.Int64 {
	if c != nil {
		return c
	}
	return m.counter()
}

func (m *shardMapOf[K, V, P, E]) loadReadOnly() readOnlyShardOf[K, V, P, E] {
	if p := m.read.Load(); p != nil {
		return *p
	}
	return readOnlyShardOf[K, V, P, E]{}
}

// empty reports that the shard holds no key: the read snapshot is empty and dirty
// adds nothing to it. Map checks it before hashing a key on the paths that can only
// miss on an empty shard, so such a lookup costs no hash. Unlike sync.Map, a key
// whose dynamic value is not hashable then misses instead of panicking.
func (m *shardMapOf[K, V, P, E]) empty() bool {
	read := m.loadReadOnly()
	return len(read.m) == 0 && !read.amended
}

// value is BOTH the cache entry AND a hash-collision chain node. Nodes are
// shared between a shardMap's read snapshot and dirty map (same pointers), so
// in-place mutation and tombstoning are visible through both views.
//
// P and E select whether a node carries valPtr and expire. gache instantiates
// them as atomic.Pointer[V] and atomic.Int64 (the shardMap and value
// aliases below), Map as struct{}: its V is a pointer, so valPtr
// is never used, and its entries never expire. The two zero-size fields take
// 16 bytes off a Map node (56 -> 40 for an int key, the 48-byte size class
// instead of 64) and leave gache's node layout unchanged.
type valueOf[K comparable, V any, P valPtrSlot[V], E expireSlot] struct {
	next   *valueOf[K, V, P, E] // next node in the same hash bucket (collision chain)
	valPtr P                    // read through vp(): a V whose size is not one machine word: &val or an immutable copy; nil from tombstone to revive
	key    K
	val    V             // word-sized V: the payload word; any other V: written once, before publication
	expire E             // read through loadExpire and exp()
	mu     sync.Mutex    // serializes writers and every state transition
	state  atomic.Uint64 // [ generation | stPtr | live/deleted/expunged ], see the st* constants
}

// valPtrSlot and expireSlot are the two node layouts: with the field (gache) or
// without it (struct{}, Map). A struct{} valPtr requires a word-sized V.
type (
	valPtrSlot[V any] interface{ atomic.Pointer[V] | struct{} }
	expireSlot        interface{ atomic.Int64 | struct{} }
)

// shardMap and value are gache's instantiation, with valPtr and
// expire.
type (
	shardMap[K comparable, V any] = shardMapOf[K, V, atomic.Pointer[V], atomic.Int64]
	value[K comparable, V any]    = valueOf[K, V, atomic.Pointer[V], atomic.Int64]
)

// vp returns the valPtr field as the atomic.Pointer[V] it is when P is not
// struct{}; nil otherwise. Callers use it only for a V that is not word-sized,
// which never has a struct{} valPtr. The size checks fold at compile time.
func (n *valueOf[K, V, P, E]) vp() *atomic.Pointer[V] {
	if unsafe.Sizeof(n.valPtr) == 0 {
		return nil
	}
	return (*atomic.Pointer[V])(unsafe.Pointer(&n.valPtr))
}

// exp returns the expire field as the atomic.Int64 it is when E is not
// struct{}; nil otherwise. Code that also runs for Map uses loadExpire and
// storeExpire instead.
func (n *valueOf[K, V, P, E]) exp() *atomic.Int64 {
	if unsafe.Sizeof(n.expire) == 0 {
		return nil
	}
	return (*atomic.Int64)(unsafe.Pointer(&n.expire))
}

// loadExpire returns expire, always 0 for a node without the field. It and
// storeExpire convert the field themselves instead of calling exp: an inlined
// generic method that calls another one costs its caller a dictionary load and
// nil check on gache's Get and Set paths.
func (n *valueOf[K, V, P, E]) loadExpire() int64 {
	if unsafe.Sizeof(n.expire) == 0 {
		return 0
	}
	return (*atomic.Int64)(unsafe.Pointer(&n.expire)).Load()
}

func loadNext[K comparable, V any, P valPtrSlot[V], E expireSlot](n *valueOf[K, V, P, E]) *valueOf[K, V, P, E] {
	return (*valueOf[K, V, P, E])(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&n.next))))
}

func storeNext[K comparable, V any, P valPtrSlot[V], E expireSlot](n *valueOf[K, V, P, E], next *valueOf[K, V, P, E]) {
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.next)), unsafe.Pointer(next))
}

// findNode walks the chain head following next and returns the first node whose
// key matches k. The caller is responsible for inspecting the node state.
func findNode[K comparable, V any, P valPtrSlot[V], E expireSlot](head *valueOf[K, V, P, E], k K) *valueOf[K, V, P, E] {
	for n := head; n != nil; n = loadNext(n) {
		if n.key == k {
			return n
		}
	}
	return nil
}

// newValue returns a live node for k that is not yet published. A V whose size
// is not one machine word is read through valPtr, which points at the node's own
// (never rewritten) val; a word-sized V records in the state whether it holds a
// pointer, decided here, on the insert path, and never on the read path.
func newValue[K comparable, V any, P valPtrSlot[V], E expireSlot](k K, val V, expire int64) *valueOf[K, V, P, E] {
	n := &valueOf[K, V, P, E]{key: k, val: val}
	if expire != 0 && unsafe.Sizeof(n.expire) != 0 {
		(*atomic.Int64)(unsafe.Pointer(&n.expire)).Store(expire)
	}
	if unsafe.Sizeof(val) != wordSize {
		n.vp().Store(&n.val)
	} else if typeHasPointers[V]() {
		// For a word-sized V the word is a pointer exactly when PtrBytes != 0
		// (pointers are word-sized and word-aligned, so any pointer in V fills
		// the whole word). typeHasPointers reads PtrBytes without allocating,
		// unlike a reflect walk, which this insert path must not pay.
		n.state.Store(stPtr)
	}
	return n
}

// live reports whether n currently holds a published entry.
func (n *valueOf[K, V, P, E]) live() bool {
	return n.state.Load()&stMask == stLive
}

// loadWord atomically loads the payload word of a word-sized V; s is the state
// the caller loaded. A pointer is loaded as a pointer, never as an integer.
func (n *valueOf[K, V, P, E]) loadWord(s uint64) (val V) {
	if s&stPtr != 0 {
		*(*unsafe.Pointer)(unsafe.Pointer(&val)) = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&n.val)))
		return val
	}
	*(*uint64)(unsafe.Pointer(&val)) = atomic.LoadUint64((*uint64)(unsafe.Pointer(&n.val)))
	return val
}

// storeWord atomically stores the payload word of a word-sized V. A pointer goes
// through a barriered pointer store so the GC sees the new pointee and shades the
// old one. Caller holds n.mu.
func (n *valueOf[K, V, P, E]) storeWord(val V) {
	if n.state.Load()&stPtr != 0 {
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&n.val)), *(*unsafe.Pointer)(unsafe.Pointer(&val)))
		return
	}
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&n.val)), *(*uint64)(unsafe.Pointer(&val)))
}

// readValue is the lock-free seqlock read of val and expire. It never returns the
// payload of another incarnation or a cleared one; val and expire are each atomic
// but not a consistent pair across an in-place overwrite of a live node. ok is
// false if the node is not live (or was tombstoned while reading); val is then
// the zero V.
func (n *valueOf[K, V, P, E]) readValue() (val V, expire int64, ok bool) {
	for {
		s := n.state.Load()
		if s&stMask != stLive {
			break
		}
		var v V
		if unsafe.Sizeof(v) == wordSize {
			v = n.loadWord(s)
		} else if vp := n.vp().Load(); vp != nil {
			v = *vp
		} else {
			break // tombstoned since s was loaded
		}
		e := n.loadExpire()
		if n.state.Load() == s {
			return v, e, true
		}
	}
	return val, 0, false
}

// loadLive is readValue without expire for range loops: its first seqlock attempt
// is small enough to inline for a word-sized V, and any other case, or a failed
// attempt, falls back to readValue.
func (n *valueOf[K, V, P, E]) loadLive() (val V, ok bool) {
	if unsafe.Sizeof(val) == wordSize {
		if s := n.state.Load(); s&stMask == stLive {
			if val = n.loadWord(s); n.state.Load() == s {
				return val, true
			}
		}
	}
	val, _, ok = n.readValue()
	return val, ok
}

// readValRaw reads the payload of the live node n. Caller holds n.mu.
func (n *valueOf[K, V, P, E]) readValRaw() V {
	if unsafe.Sizeof(n.val) == wordSize {
		return n.val
	}
	return *n.vp().Load()
}

// tombstone moves the live node n to deleted and decrements c, the length
// counter. The new state (generation + 1) is stored BEFORE the payload is
// cleared, which is what lets seqlock readers reject a cleared payload.
// Caller holds n.mu.
func (n *valueOf[K, V, P, E]) tombstone(c *atomic.Int64) {
	s := n.state.Load()
	n.state.Store((s&^stMask + stGen) | stDeleted)
	if unsafe.Sizeof(n.val) == wordSize {
		var zero V
		n.storeWord(zero)
	} else {
		n.vp().Store(nil) // n.val is never rewritten, readers may still hold &n.val
	}
	c.Add(-1)
}

// revive moves the deleted node n back to live in place and increments c, the
// length counter of the read snapshot n was found in. Payload and expire are
// written BEFORE the live state is stored, so a reader that sees the live state
// sees them too. The generation stays the one the tombstone assigned: no live
// state of an earlier incarnation can recur. Caller holds n.mu.
func (n *valueOf[K, V, P, E]) revive(val V, expire int64, c *atomic.Int64) {
	n.writeVal(val)
	n.storeExpire(expire)
	n.state.Store(n.state.Load() &^ stMask)
	c.Add(1)
}

// tryExpunge reports whether n is expunged, expunging it first when it is
// deleted. A live node (never tombstoned, or revived meanwhile) is left alone and
// reported false; the transition is decided under n.mu, so a reviver either
// wins and the node stays in dirty, or finds it expunged. Caller holds m.mu.
func (n *valueOf[K, V, P, E]) tryExpunge() bool {
	if n.live() {
		return false
	}
	n.mu.Lock()
	s := n.state.Load()
	if s&stMask == stLive {
		n.mu.Unlock()
		return false
	}
	if s&stMask == stDeleted {
		n.state.Store(s&^stMask | stExpunged)
	}
	n.mu.Unlock()
	return true
}

// writeVal writes val to the live or deleted node n. A V whose size is not one
// machine word is never written in place: it is copied to a fresh V that valPtr
// then points at, so lock-free readers dereferencing an older valPtr always see a
// whole value. Caller holds n.mu.
func (n *valueOf[K, V, P, E]) writeVal(val V) {
	if unsafe.Sizeof(val) == wordSize {
		n.storeWord(val)
		return
	}
	if vp := n.vp().Load(); vp != nil && valEqualPtr(vp, &val) {
		return
	}
	// n.val is never rewritten, so a revival (or overwrite) back to it can point
	// valPtr at it again instead of allocating a copy.
	if valEqualPtr(&n.val, &val) {
		n.vp().Store(&n.val)
		return
	}
	newVal := new(V)
	*newVal = val
	n.vp().Store(newVal)
}

// storeExpire stores expire only if it differs from the current value. A node
// without the field has nothing to store: Map always passes 0.
func (n *valueOf[K, V, P, E]) storeExpire(expire int64) {
	if unsafe.Sizeof(n.expire) == 0 {
		return
	}
	if e := (*atomic.Int64)(unsafe.Pointer(&n.expire)); e.Load() != expire {
		e.Store(expire)
	}
}

// tryStore sets val/expire on n: an overwrite when n is live, a revival when it
// is deleted. ok is false if n is expunged, in which case the caller must go
// through m.mu. It is not upsert(..., true): that also reads and returns the
// previous V, which Set never needs (+11.6% on a 256-byte V overwrite; benchstat,
// interleaved A/B, 2026-11-09, mission gache-critical-path-slim).
func (n *valueOf[K, V, P, E]) tryStore(val V, expire int64, c *atomic.Int64) (ok bool) {
	if unsafe.Sizeof(val) != twoWordSize {
		n.mu.Lock()
	} else {
		// Writers of one value to a node that expired under them arrive together
		// (store's lock-free same-value path needs a live node). Queued on n.mu,
		// each would wait behind the one reviving it, and a convoy forms on every
		// key; polling while the node is dead lets the others take that path as
		// soon as it is live with val again. A live node with another value is
		// waited for as before.
		for !n.mu.TryLock() {
			if vp := n.vp().Load(); vp != nil && n.live() {
				if valEqualPtr(vp, &val) {
					n.storeExpire(expire)
					return true
				}
				n.mu.Lock()
				break
			}
			runtime.Gosched()
		}
	}
	switch n.state.Load() & stMask {
	case stLive:
		n.writeVal(val)
		n.storeExpire(expire)
	case stDeleted:
		n.revive(val, expire, c)
	default:
		n.mu.Unlock()
		return false
	}
	n.mu.Unlock()
	return true
}

// upsert is tryStore that also reports the replaced value. A live n yields its
// current value (loaded) and is only overwritten when overwrite is set; a deleted
// n is revived (loaded false). ok is false if n is expunged.
func (n *valueOf[K, V, P, E]) upsert(val V, expire int64, c *atomic.Int64, overwrite bool) (prev V, loaded, ok bool) {
	n.mu.Lock()
	switch n.state.Load() & stMask {
	case stLive:
		prev, loaded = n.readValRaw(), true
		if overwrite {
			n.writeVal(val)
			n.storeExpire(expire)
		}
	case stDeleted:
		n.revive(val, expire, c)
	default:
		n.mu.Unlock()
		return prev, false, false
	}
	n.mu.Unlock()
	return prev, loaded, true
}

// tombstoneIf tombstones n, decrementing c, when cond is nil or holds for its
// value. live is false if n was not live. With expunge set the node goes straight
// to expunged in the same n.mu critical section, so a caller that unlinks it from
// dirty never leaves a revivable node behind. cond runs under n.mu.
func (n *valueOf[K, V, P, E]) tombstoneIf(c *atomic.Int64, cond func(V) bool, expunge bool) (val V, expire int64, loaded, live bool) {
	n.mu.Lock()
	return n.tombstoneIfLocked(c, cond, expunge)
}

// tombstoneIfLocked is tombstoneIf with n.mu already held; it unlocks n.mu.
func (n *valueOf[K, V, P, E]) tombstoneIfLocked(c *atomic.Int64, cond func(V) bool, expunge bool) (val V, expire int64, loaded, live bool) {
	if !n.live() {
		n.mu.Unlock()
		return val, 0, false, false
	}
	cur := n.readValRaw()
	if cond != nil && !cond(cur) {
		n.mu.Unlock()
		return val, 0, false, true
	}
	expire = n.loadExpire()
	n.tombstone(c)
	if expunge {
		n.state.Store(n.state.Load()&^stMask | stExpunged)
	}
	n.mu.Unlock()
	return cur, expire, true, true
}

// tombstoneIfExpired is tombstoneIf for expiry eviction: n is tombstoned only if it
// is live and expired at now (0 < expire < now). A node that was overwritten with a
// fresh value, or revived, since the caller saw it expired is left alone (live
// true, loaded false). A writer holding n.mu cannot change expire under it; the
// lock-free same-value overwrite of a two-word V (store) can, in the few
// instructions between the check and the tombstone.
//
// Readers that saw one entry expired arrive at its node together. Waiting for
// n.mu would queue each behind the one tombstoning it, only to find the node dead
// under the lock; polling lets them leave as soon as the node is dead or fresh,
// with the result they would get under the lock.
func (n *valueOf[K, V, P, E]) tombstoneIfExpired(c *atomic.Int64, now int64, expunge bool) (val V, loaded, live bool) {
	expired := func(V) bool {
		exp := n.loadExpire()
		return exp > 0 && now > exp
	}
	for !n.mu.TryLock() {
		if !n.live() {
			return val, false, false
		}
		if !expired(val) {
			return val, false, true
		}
		runtime.Gosched()
	}
	val, _, loaded, live = n.tombstoneIfLocked(c, expired, expunge)
	return val, loaded, live
}

// valEqualPtr checks if *a and *b have identical 16-byte memory representation.
func valEqualPtr[V any](a, b *V) bool {
	if unsafe.Sizeof(*a) == twoWordSize {
		p1 := (*[2]uint64)(unsafe.Pointer(a))
		p2 := (*[2]uint64)(unsafe.Pointer(b))
		return p1[0] == p2[0] && p1[1] == p2[1]
	}
	return false
}

// loadValue returns the value and expire for k, read lock-free as readValue does
// (each atomic, not a consistent pair across an in-place overwrite). ok is false
// if the key is absent or tombstoned.
func (m *shardMapOf[K, V, P, E]) loadValue(h uint64, k K) (val V, expire int64, ok bool) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		// First attempt of readValue's seqlock, inlined by hand: Get is the hot
		// path and readValue is too big to inline. A nil valPtr means a tombstone
		// ran after s was loaded, so the final state check fails; if that happens,
		// or the node is not live, readValue below takes over.
		if s := n.state.Load(); s&stMask == stLive {
			if unsafe.Sizeof(val) == wordSize {
				val = n.loadWord(s)
			} else if vp := n.vp().Load(); vp != nil {
				val = *vp
			}
			if expire = n.loadExpire(); n.state.Load() == s {
				return val, expire, true
			}
		}
		if v, e, ok := n.readValue(); ok {
			return v, e, true
		}
	}
	if read.amended {
		n := m.lockSlow(h, k)
		m.mu.Unlock()
		if n != nil {
			return n.readValue()
		}
	}
	var zero V
	return zero, 0, false
}

// lockSlow is the amended-miss lookup shared by loadValue and loadNode. It takes
// m.mu and, with it held, re-checks the read snapshot and then the dirty map for a
// live node for k. It returns that node (nil if none) and leaves m.mu LOCKED: the
// caller must Unlock it. A read-snapshot miss counts towards promoting dirty
// (missLocked).
func (m *shardMapOf[K, V, P, E]) lockSlow(h uint64, k K) *valueOf[K, V, P, E] {
	m.mu.Lock()
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil && n.live() {
		return n
	}
	if read.amended {
		nDirty := findNode(m.dirty[h], k)
		m.missLocked()
		if nDirty != nil && nDirty.live() {
			return nDirty
		}
	}
	return nil
}

// loadNode returns the live node for k, or nil. Used by callers that need the
// node identity (e.g. expiration checks, in-place refresh).
func (m *shardMapOf[K, V, P, E]) loadNode(h uint64, k K) (*valueOf[K, V, P, E], bool) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil && n.live() {
		return n, true
	}
	if !read.amended {
		return nil, false
	}
	n := m.lockSlow(h, k)
	m.mu.Unlock()
	return n, n != nil
}

// store sets k=val/expire. A live node is overwritten and a deleted node is
// revived in place, both under the node's lock only. Overwriting a live node is
// 0-alloc only for a word-sized V or an unchanged two-word V; any other overwrite
// allocates one V (writeVal; a zero-size V allocates nothing). An expunged node or
// a new key falls to the slow path, which holds m.mu.
func (m *shardMapOf[K, V, P, E]) store(h uint64, k K, val V, expire int64) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		if unsafe.Sizeof(val) != wordSize {
			if vp := n.vp().Load(); vp != nil && n.live() && valEqualPtr(vp, &val) {
				n.storeExpire(expire)
				return
			}
		}
		if n.tryStore(val, expire, m.counterFor(read.c)) {
			return
		}
	}
	m.upsertSlow(h, k, val, expire, true)
}

// upsert stores val/expire for k and returns the previous value when k was live.
// With overwrite unset a live k is left as is (LoadOrStore); a deleted node is
// revived in place and an expunged one or a new key falls to upsertSlow.
func (m *shardMapOf[K, V, P, E]) upsert(h uint64, k K, val V, expire int64, overwrite bool) (prev V, loaded bool) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		if !overwrite {
			if cur, _, ok := n.readValue(); ok {
				return cur, true
			}
		}
		if prev, loaded, ok := n.upsert(val, expire, m.counterFor(read.c), overwrite); ok {
			return prev, loaded
		}
	}
	return m.upsertSlow(h, k, val, expire, overwrite)
}

// upsertSlow is the single-m.mu slow path of store and upsert: it handles a live,
// a deleted (revived in place) and an absent (fresh node into dirty) key. A node
// found expunged is superseded by that fresh node.
func (m *shardMapOf[K, V, P, E]) upsertSlow(h uint64, k K, val V, expire int64, overwrite bool) (prev V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if n := findNode(m.headLocked(read, h), k); n != nil {
		if prev, loaded, ok := n.upsert(val, expire, m.counterFor(read.c), overwrite); ok {
			if read.amended {
				m.missLocked()
			}
			return prev, loaded
		}
	}
	if !read.amended {
		m.amendLocked(read)
	}
	nn := newValue[K, V, P, E](k, val, expire)
	if head := m.dirty[h]; head != nil {
		m.chained++
		storeNext(nn, head)
	}
	m.dirty[h] = nn
	m.counter().Add(1)
	return prev, false
}

// loadOrStore returns the existing live value if present, otherwise it stores
// val/expire.
func (m *shardMapOf[K, V, P, E]) loadOrStore(h uint64, k K, val V, expire int64) (actual V, loaded bool) {
	if prev, loaded := m.upsert(h, k, val, expire, false); loaded {
		return prev, true
	}
	return val, false
}

// updateIf replaces the value of the live node n with next() if match holds for
// its current value; both run under n.mu. ok is false if n was tombstoned or match
// declined. Map's compare-and-swap rejects a mismatch from a lock-free read before
// calling it, so failing loops do not serialize on the node lock.
func (n *valueOf[K, V, P, E]) updateIf(match func(cur V) bool, next func() V) (ok bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.live() || !match(n.readValRaw()) {
		return false
	}
	n.writeVal(next())
	return true
}

// headLocked returns the chain head for h in the map that writers use under
// m.mu: the dirty map when read is amended (it then holds every node), else the
// read snapshot.
func (m *shardMapOf[K, V, P, E]) headLocked(read readOnlyShardOf[K, V, P, E], h uint64) *valueOf[K, V, P, E] {
	if read.amended {
		return m.dirty[h]
	}
	return read.m[h]
}

// amendLocked rebuilds the dirty map and marks read, the non-amended snapshot
// loaded under m.mu, as amended. Caller holds m.mu.
func (m *shardMapOf[K, V, P, E]) amendLocked(read readOnlyShardOf[K, V, P, E]) {
	m.dirtyLocked()
	m.read.Store(&readOnlyShardOf[K, V, P, E]{m: read.m, amended: true, c: m.counterFor(read.c)})
}

// loadAndDelete tombstones the live node for k and returns its value. Unlinking
// is lazy for a node in the read snapshot (the next dirtyLocked drops it) and
// eager (popSlow) for a node that only lives in the dirty map.
func (m *shardMapOf[K, V, P, E]) loadAndDelete(h uint64, k K) (val V, loaded bool) {
	val, _, loaded = m.popIf(h, k, nil)
	return val, loaded
}

// popIf tombstones the live node for k and returns its value and expire when cond
// is nil or holds for the value. Unlinking is lazy for a node in the read
// snapshot (the next dirtyLocked drops it) and eager for a node that only lives
// in the dirty map: popSlow unlinks (and expunges) it, so churning unique keys
// does not accumulate tombstones there until the next promotion.
func (m *shardMapOf[K, V, P, E]) popIf(h uint64, k K, cond func(V) bool) (val V, expire int64, loaded bool) {
	read := m.loadReadOnly()
	// A dead node is skipped without taking its lock: nothing to pop.
	if n := findNode(read.m[h], k); n != nil && n.live() {
		if cond != nil {
			// Lock-free reject so failing compare-and-delete loops do not
			// serialize on the node lock; cond is evaluated again under it.
			if cur, _, live := n.readValue(); live && !cond(cur) {
				return val, 0, false
			}
		}
		if val, expire, loaded, live := n.tombstoneIf(m.counterFor(read.c), cond, false); live {
			return val, expire, loaded
		}
	}
	if !read.amended {
		return val, 0, false
	}
	return m.popSlow(h, k, cond)
}

// popSlow is popIf's amended-miss path: under m.mu it looks the node up in the
// read snapshot and then in dirty (evictSlow) and pops it with tombstoneIf.
func (m *shardMapOf[K, V, P, E]) popSlow(h uint64, k K, cond func(V) bool) (val V, expire int64, loaded bool) {
	return m.evictSlow(h, k, func(n *valueOf[K, V, P, E], c *atomic.Int64, expunge bool) (V, int64, bool, bool) {
		return n.tombstoneIf(c, cond, expunge)
	})
}

// evictSlow is the amended-miss path shared by popSlow and expireSlow: under m.mu
// it looks the node up in the read snapshot and then in dirty, and tombstones it
// with tombstone, the node-level step (tombstoneIf or tombstoneIfExpired). The step
// runs under n.mu and reports the value and expire of the node it tombstoned, loaded
// when it did, and live when the node was live at all. The step does not escape.
func (m *shardMapOf[K, V, P, E]) evictSlow(
	h uint64,
	k K,
	tombstone func(n *valueOf[K, V, P, E], c *atomic.Int64, expunge bool) (val V, expire int64, loaded, live bool),
) (val V, expire int64, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		if v, e, ok, live := tombstone(n, m.counterFor(read.c), false); live {
			return v, e, ok
		}
	}
	if !read.amended {
		return val, 0, false
	}
	if n := findNode(m.dirty[h], k); n != nil {
		// n is usually dirty-only, but it can also be a node shared with read that a
		// fast path revived after the read lookup above saw it dead. Expunging it in
		// the tombstoning critical section makes the unlink safe either way: nobody
		// can revive it behind the unlinked chain's back (a revived, unlinked node
		// would be live in read but missing from dirty and lost on promotion).
		if v, e, ok, live := tombstone(n, m.counterFor(read.c), true); live && ok {
			m.unlinkDirtyLocked(h, n)
			// The delete shrank dirty and left read exactly as stale as it was, so
			// it does not count as a miss: counting it would promote and rebuild
			// dirty on every Set/Delete of a fresh key.
			return v, e, true
		}
	}
	m.missLocked()
	return val, 0, false
}

// expireIf is popIf for expiry eviction: it tombstones the live node for k and
// returns its value only if the node is still expired at now (0 < expire < now). A
// reader that saw k expired evicts it here a moment later; a Set that landed in
// between has replaced the expired value, and popIf would take that fresh value
// away. Unlinking is the same as popIf's. It mirrors popIf rather than
// parametrizing it so that Delete's fast path stays untouched; the slow paths
// share evictSlow.
func (m *shardMapOf[K, V, P, E]) expireIf(h uint64, k K, now int64) (val V, loaded bool) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil && n.live() {
		if val, loaded, live := n.tombstoneIfExpired(m.counterFor(read.c), now, false); live {
			return val, loaded
		}
	}
	if !read.amended {
		return val, false
	}
	return m.expireSlow(h, k, now)
}

// expireSlow is expireIf's amended-miss path, popSlow with an expiry condition.
func (m *shardMapOf[K, V, P, E]) expireSlow(h uint64, k K, now int64) (val V, loaded bool) {
	val, _, loaded = m.evictSlow(h, k, func(n *valueOf[K, V, P, E], c *atomic.Int64, expunge bool) (V, int64, bool, bool) {
		v, ok, live := n.tombstoneIfExpired(c, now, expunge)
		return v, 0, ok, live
	})
	return val, loaded
}

// unlinkDirtyLocked removes n from the dirty chain for h. n.next is left intact.
// n must already be expunged (tombstoneIf with expunge), so nobody can revive a
// node that is no longer reachable from dirty. Caller holds m.mu.
func (m *shardMapOf[K, V, P, E]) unlinkDirtyLocked(h uint64, n *valueOf[K, V, P, E]) {
	var prev *valueOf[K, V, P, E]
	for cur := m.dirty[h]; cur != nil; cur = loadNext(cur) {
		if cur != n {
			prev = cur
			continue
		}
		switch next := loadNext(n); {
		case prev != nil:
			storeNext(prev, next)
			m.chained--
		case next != nil:
			m.dirty[h] = next
			m.chained--
		default:
			delete(m.dirty, h) // the bucket goes with its only node: chained is unchanged
		}
		return
	}
}

func (m *shardMapOf[K, V, P, E]) missLocked() {
	m.misses++
	if m.misses < len(m.dirty) {
		return
	}
	m.promoteLocked()
}

// promoteLocked publishes the dirty map as the new read snapshot. Caller holds m.mu.
func (m *shardMapOf[K, V, P, E]) promoteLocked() {
	m.read.Store(&readOnlyShardOf[K, V, P, E]{m: m.dirty, c: m.counter()})
	m.dirty = nil
	m.misses = 0
}

// dirtyLocked rebuilds the dirty map from the read snapshot without breaking
// pointer identity. Every deleted node it skips or unlinks is expunged first
// (tryExpunge), so a node a concurrent writer revived in the meantime stays in
// dirty and an expunged one can never be revived behind the rebuilt map's back.
// Leading expunged nodes are skipped and fully dead chains are completely dropped
// so that when m.dirty is promoted to read, the dead nodes and old map snapshots
// are reclaimed by the Go runtime GC. Interspersed and trailing expunged nodes
// are unlinked in-place atomically (their own next stays intact for readers
// standing on them).
func (m *shardMapOf[K, V, P, E]) dirtyLocked() {
	read := m.loadReadOnly()
	m.dirty = make(map[uint64]*valueOf[K, V, P, E], len(read.m))
	m.chained = 0
	for h, head := range read.m {
		for head != nil && head.tryExpunge() {
			head = loadNext(head)
		}
		if head == nil {
			// All nodes in this chain were dead; omit h from dirty so that upon
			// promotion the chain is dropped and reclaimed by GC.
			continue
		}
		prev := head
		for curr := loadNext(head); curr != nil; {
			next := loadNext(curr)
			if curr.tryExpunge() {
				storeNext(prev, next)
			} else {
				prev = curr
				m.chained++
			}
			curr = next
		}

		// Share head directly. NEVER allocate new value[K, V] nodes here.
		// Preserving pointer identity across read.m and dirty is essential so that
		// concurrent in-place overwrites and deletes on surviving keys operate on
		// the exact same heap nodes.
		m.dirty[h] = head
	}
}

// readSnapshot returns the current read-only snapshot. If amended, it promotes
// the dirty map under lock first.
func (m *shardMapOf[K, V, P, E]) readSnapshot() readOnlyShardOf[K, V, P, E] {
	read := m.loadReadOnly()
	if read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		if read.amended {
			m.promoteLocked()
			read = m.loadReadOnly()
		}
		m.mu.Unlock()
	}
	return read
}

// rangeShard iterates the read snapshot's chains, skipping dead nodes, and
// calls f with the original key and the node. If the snapshot is amended, it is
// promoted first so all keys are observable.
func (m *shardMapOf[K, V, P, E]) rangeShard(f func(k K, n *valueOf[K, V, P, E])) {
	for _, head := range m.readSnapshot().m {
		for n := head; n != nil; n = loadNext(n) {
			if n.live() {
				f(n.key, n)
			}
		}
	}
}

// rangeShardVal iterates the read snapshot's chains, skipping dead and expired
// nodes, and calls f with the original key, value and expire of one readValue:
// never another incarnation's payload, but val and expire are not a consistent
// pair across an in-place overwrite of a live node.
func (m *shardMapOf[K, V, P, E]) rangeShardVal(now int64, f func(k K, val V, exp int64)) {
	for _, head := range m.readSnapshot().m {
		for n := head; n != nil; n = loadNext(n) {
			if val, exp, ok := n.readValue(); ok && (exp <= 0 || now <= exp) {
				f(n.key, val, exp)
			}
		}
	}
}

// deleteExpired purges all expired nodes directly in place within the shard.
// It avoids re-hashing keys and re-traversing collision chains. It walks the
// snapshot itself because it needs the snapshot's length counter (see tombstone).
// If onExpired is non-nil, it is called for each expired entry with its key and value.
// It returns the total number of expired entries purged in this shard.
func (m *shardMapOf[K, V, P, E]) deleteExpired(now int64, onExpired func(K, V)) uint64 {
	read := m.readSnapshot()
	c := m.counterFor(read.c)
	var count uint64
	for _, head := range read.m {
		for n := head; n != nil; n = loadNext(n) {
			if !n.live() {
				continue
			}
			exp := n.loadExpire()
			if exp <= 0 || now <= exp {
				continue
			}
			n.mu.Lock()
			exp = n.loadExpire()
			if n.live() && exp > 0 && now > exp {
				val := n.readValRaw()
				n.tombstone(c)
				count++
				k := n.key
				n.mu.Unlock()
				if onExpired != nil {
					onExpired(k, val)
				}
			} else {
				n.mu.Unlock()
			}
		}
	}
	return count
}

func (m *shardMapOf[K, V, P, E]) length() int {
	c := m.l.Load()
	if c == nil {
		return 0
	}
	l := int(c.Load())
	if l < 0 {
		return 0
	}
	return l
}

func (m *shardMapOf[K, V, P, E]) clear() {
	read := m.loadReadOnly()
	if len(read.m) == 0 && !read.amended && m.length() == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	read = m.loadReadOnly()
	if len(read.m) == 0 && !read.amended && m.length() == 0 {
		return
	}
	c := new(atomic.Int64)
	m.l.Store(c)
	m.read.Store(&readOnlyShardOf[K, V, P, E]{c: c})
	m.dirty = nil
	m.misses = 0
	m.chained = 0
}

func (m *shardMapOf[K, V, P, E]) initReserve(size int) {
	m.mu.Lock()
	if m.dirty == nil && len(m.loadReadOnly().m) == 0 {
		m.dirty = make(map[uint64]*valueOf[K, V, P, E], size)
		m.read.Store(&readOnlyShardOf[K, V, P, E]{m: map[uint64]*valueOf[K, V, P, E]{}, amended: true, c: m.counter()})
	}
	m.mu.Unlock()
}

// size estimates the bytes owned by this shard: the shardMap struct, the length
// counter, the read and dirty map structures, and the value[K, V] nodes. It takes
// m.mu briefly and allocates nothing. Its cost is O(swiss directory) of the two
// bucket maps (mapSize visits each directory entry once), not O(entries): the
// nodes are counted from the bucket count, not walked.
//
// Nodes are shared between read and dirty, so they are charged once, from the one
// structure that holds all of them: while dirty is non-nil every non-expunged node
// reachable from read is also reachable from dirty (see the invariant above), so
// that structure is dirty; otherwise it is read. Its node count is exact:
// buckets plus m.chained, the nodes that sit behind a bucket's head. Hash
// collisions are not negligible in gache (keys are hashed by their first
// WithMaxKeyLength bytes, and by a 64-byte sample when longer than 64), so
// counting one node per bucket would miss every chained node.
//
// m.chained is maintained under m.mu only, never on the Get, overwrite and Delete
// fast paths, which do not change any chain: an insert onto a non-empty dirty
// chain adds one, unlinkDirtyLocked removes one when its chain stays non-empty,
// dirtyLocked recounts while it walks every chain, clear resets it. promoteLocked
// publishes dirty as read unchanged, so the count carries over; the read chains
// are not touched between a promotion and the next dirtyLocked (inserts go to
// dirty, unlinks only happen while dirty exists).
//
// Not charged: expunged nodes that only a stale read snapshot still reaches
// (leading nodes dirtyLocked skipped, or a shared node evictSlow unlinked from
// dirty). They are dead, cannot be revived and are dropped at the next
// promotion; every live or tombstoned node is charged.
//
// Each heap object is charged as the allocator rounds it (allocSize: size class,
// malloc header, pages), not by unsafe.Sizeof: a 40-byte Map node takes 48 bytes.
// The shardMap struct itself is charged unrounded: it lives inside a Map or in
// gache's shard arena, not in an allocation of its own.
func (m *shardMapOf[K, V, P, E]) size() (size uintptr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	size = unsafe.Sizeof(*m)

	if m.l.Load() != nil {
		size += allocSize(unsafe.Sizeof(atomic.Int64{}), false)
	}

	if m.read.Load() != nil { // a shard never written to has no snapshot yet
		size += allocSize(unsafe.Sizeof(readOnlyShardOf[K, V, P, E]{}), true)
	}
	read := m.loadReadOnly()
	size += mapSizeScan(read.m, true)
	size += mapSizeScan(m.dirty, true)
	buckets := len(read.m)
	if m.dirty != nil {
		buckets = len(m.dirty)
	}
	size += uintptr(buckets+m.chained) * allocSize(unsafe.Sizeof(valueOf[K, V, P, E]{}), true)
	return size
}
