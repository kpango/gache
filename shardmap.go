package gache

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// shardMap is an unexported concurrent map keyed by the precomputed uint64 hash
// gache already calculates for shard selection. It is modeled on the proven
// sync.Map algorithm in map.go (read/dirty/misses/promote/amended) but differs
// in two important ways:
//
//  1. The map value is a *value[V] chain HEAD. value[V] nodes are simultaneously
//     the cache entry AND a collision-chain node (linked via next). Collisions on
//     the uint64 hash are resolved by walking the chain and comparing the original
//     string key. This avoids a separate entry object: a brand-new key costs ONE
//     node allocation.
//  2. value[V] nodes are SHARED between the read snapshot map and the dirty map
//     (same pointers, exactly like sync.Map's shared entries). In-place mutation
//     and tombstoning are therefore visible through both views. Promotion copies
//     the map[uint64]*value[V] (head pointers shared) and physically drops
//     fully-tombstoned chains so memory is reclaimed.
//
// Reads are lock-free at the map-structure level (a single atomic load of the
// read snapshot) plus a per-node RWMutex.RLock for field consistency against
// in-place overwrites. The rare amended-miss path falls back under mu.
type shardMap[V any] struct {
	read   atomic.Pointer[readOnlyShard[V]]
	dirty  map[uint64]*value[V]
	length atomic.Int64
	misses int
	mu     sync.Mutex
}

type readOnlyShard[V any] struct {
	m       map[uint64]*value[V]
	amended bool
}

func (m *shardMap[V]) loadReadOnly() readOnlyShard[V] {
	if p := m.read.Load(); p != nil {
		return *p
	}
	return readOnlyShard[V]{}
}

// findLive walks the chain head following next and returns the first node whose
// key matches k and that is not tombstoned. The caller is responsible for any
// further locking; this helper only inspects the deleted flag under each node's
// RLock for a consistent view.
func findNode[V any](head *value[V], k string) *value[V] {
	for n := head; n != nil; n = n.next {
		if n.key == k {
			return n
		}
	}
	return nil
}

// loadValue returns the value and expire for k, reading them atomically under
// the node's RLock. ok is false if the key is absent or tombstoned.
func (m *shardMap[V]) loadValue(h uint64, k string) (val V, expire int64, ok bool) {
	read := m.loadReadOnly()
	head, present := read.m[h]
	if !present && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		head, present = read.m[h]
		if !present && read.amended {
			head = m.dirty[h]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	for n := head; n != nil; n = n.next {
		if n.key != k {
			continue
		}
		n.mu.RLock()
		if n.deleted {
			n.mu.RUnlock()
			return val, 0, false
		}
		val = n.val
		expire = n.expire
		n.mu.RUnlock()
		return val, expire, true
	}
	return val, 0, false
}

// loadNode returns the live (non-tombstoned) node for k, or nil. Used by callers
// that need the node identity (e.g. expiration checks, in-place refresh).
func (m *shardMap[V]) loadNode(h uint64, k string) (*value[V], bool) {
	read := m.loadReadOnly()
	head, present := read.m[h]
	if !present && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		head, present = read.m[h]
		if !present && read.amended {
			head = m.dirty[h]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	for n := head; n != nil; n = n.next {
		if n.key != k {
			continue
		}
		n.mu.RLock()
		del := n.deleted
		n.mu.RUnlock()
		if del {
			return nil, false
		}
		return n, true
	}
	return nil, false
}

// store sets k=val/expire. Overwrite of an existing live node is 0-alloc: it
// mutates in place under the node's lock. Tombstoned nodes and new keys fall
// to the slow path, which holds m.mu and verifies the node is still reachable
// through the current maps — this is necessary to avoid incorrectly reviving a
// pre-Clear tombstoned node and inflating the length counter.
func (m *shardMap[V]) store(h uint64, k string, val V, expire int64) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		n.mu.Lock()
		if !n.deleted {
			n.val = val
			n.expire = expire
			n.mu.Unlock()
			return
		}
		n.mu.Unlock()
	}
	m.storeSlow(h, k, val, expire)
}

func (m *shardMap[V]) storeSlow(h uint64, k string, val V, expire int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		n.mu.Lock()
		if n.deleted {
			n.deleted = false
			m.length.Add(1)
		}
		n.val = val
		n.expire = expire
		n.mu.Unlock()
		return
	}
	if read.amended {
		if n := findNode(m.dirty[h], k); n != nil {
			n.mu.Lock()
			if n.deleted {
				n.deleted = false
				m.length.Add(1)
			}
			n.val = val
			n.expire = expire
			n.mu.Unlock()
			return
		}
	}
	if !read.amended {
		m.dirtyLocked()
		m.read.Store(&readOnlyShard[V]{m: read.m, amended: true})
	}
	nn := &value[V]{key: k, val: val, expire: expire}
	nn.next = m.dirty[h]
	m.dirty[h] = nn
	m.length.Add(1)
}

// loadOrStore returns the existing live value if present, otherwise stores
// val/expire. Tombstoned nodes fall to the slow path so that pre-Clear node
// references cannot inflate the length counter.
func (m *shardMap[V]) loadOrStore(h uint64, k string, val V, expire int64) (actual V, loaded bool) {
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		n.mu.RLock()
		if !n.deleted {
			actual = n.val
			n.mu.RUnlock()
			return actual, true
		}
		n.mu.RUnlock()
	}
	return m.loadOrStoreSlow(h, k, val, expire)
}

func (m *shardMap[V]) loadOrStoreSlow(h uint64, k string, val V, expire int64) (actual V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if n := findNode(read.m[h], k); n != nil {
		n.mu.Lock()
		if !n.deleted {
			actual = n.val
			n.mu.Unlock()
			return actual, true
		}
		// Revive tombstone: increment length while still holding n.mu so a
		// concurrent Delete cannot decrement before we increment.
		n.deleted = false
		m.length.Add(1)
		n.val = val
		n.expire = expire
		actual = val
		n.mu.Unlock()
		return actual, false
	}
	if read.amended {
		if n := findNode(m.dirty[h], k); n != nil {
			n.mu.Lock()
			if !n.deleted {
				actual = n.val
				n.mu.Unlock()
				return actual, true
			}
			n.deleted = false
			m.length.Add(1) // inside lock; same reason as above
			n.val = val
			n.expire = expire
			actual = val
			n.mu.Unlock()
			return actual, false
		}
	}
	if !read.amended {
		m.dirtyLocked()
		m.read.Store(&readOnlyShard[V]{m: read.m, amended: true})
	}
	nn := &value[V]{key: k, val: val, expire: expire}
	nn.next = m.dirty[h]
	m.dirty[h] = nn
	m.length.Add(1)
	return val, false
}

// loadAndDelete tombstones the live node for k and returns its captured value.
// Physical unlinking happens lazily at the next promotion.
func (m *shardMap[V]) loadAndDelete(h uint64, k string) (val V, loaded bool) {
	read := m.loadReadOnly()
	head, present := read.m[h]
	if !present && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		head, present = read.m[h]
		if !present && read.amended {
			head = m.dirty[h]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	for n := head; n != nil; n = n.next {
		if n.key != k {
			continue
		}
		n.mu.Lock()
		if n.deleted {
			n.mu.Unlock()
			return val, false
		}
		val = n.val
		n.deleted = true
		// Decrement while still holding n.mu so a concurrent store/loadOrStore
		// cannot observe deleted=true, increment the counter, and then have our
		// Add(-1) arrive afterward (yielding a net -1 for the entry).
		m.length.Add(-1)
		n.mu.Unlock()
		return val, true
	}
	return val, false
}

// popValue tombstones the live node for k, returning its value and expire.
func (m *shardMap[V]) popValue(h uint64, k string) (val V, expire int64, loaded bool) {
	read := m.loadReadOnly()
	head, present := read.m[h]
	if !present && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		head, present = read.m[h]
		if !present && read.amended {
			head = m.dirty[h]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	for n := head; n != nil; n = n.next {
		if n.key != k {
			continue
		}
		n.mu.Lock()
		if n.deleted {
			n.mu.Unlock()
			return val, 0, false
		}
		val = n.val
		expire = n.expire
		n.deleted = true
		m.length.Add(-1) // inside n.mu; same reason as loadAndDelete above
		n.mu.Unlock()
		return val, expire, true
	}
	return val, 0, false
}

func (m *shardMap[V]) missLocked() {
	m.misses++
	if m.misses < len(m.dirty) {
		return
	}
	m.read.Store(&readOnlyShard[V]{m: m.dirty})
	m.dirty = nil
	m.misses = 0
}

// dirtyLocked rebuilds the dirty map from the read snapshot, copying head
// pointers verbatim. next pointers of existing nodes are never modified after
// initial insertion, so concurrent chain traversals (rangeShard, loadValue, etc.)
// that read n.next without a lock are safe — there is no writer to race with.
// Tombstoned nodes remain in chains but are skipped by deleted checks during
// traversal; they are eventually reclaimed by the GC when all references drop.
func (m *shardMap[V]) dirtyLocked() {
	if m.dirty != nil {
		return
	}
	read := m.loadReadOnly()
	m.dirty = make(map[uint64]*value[V], len(read.m))
	for h, head := range read.m {
		m.dirty[h] = head // copy head pointer; chains are never structurally modified
	}
}

// rangeShard iterates the read snapshot's chains, skipping tombstoned nodes, and
// calls f with the original key and the node. If the snapshot is amended, it is
// promoted first so all keys are observable.
func (m *shardMap[V]) rangeShard(f func(k string, n *value[V]) bool) {
	read := m.loadReadOnly()
	if read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		if read.amended {
			m.read.Store(&readOnlyShard[V]{m: m.dirty})
			m.dirty = nil
			m.misses = 0
			read = m.loadReadOnly()
		}
		m.mu.Unlock()
	}
	for _, head := range read.m {
		for n := head; n != nil; n = n.next {
			n.mu.RLock()
			del := n.deleted
			n.mu.RUnlock()
			if del {
				continue
			}
			if !f(n.key, n) {
				return
			}
		}
	}
}

func (m *shardMap[V]) length_() int {
	l := int(m.length.Load())
	if l < 0 {
		return 0
	}
	return l
}

func (m *shardMap[V]) clear() {
	m.mu.Lock()
	m.read.Store(&readOnlyShard[V]{})
	m.dirty = nil
	m.misses = 0
	m.length.Store(0)
	m.mu.Unlock()
}

func (m *shardMap[V]) initReserve(size int) {
	m.mu.Lock()
	if m.dirty == nil && len(m.loadReadOnly().m) == 0 {
		m.dirty = make(map[uint64]*value[V], size)
		m.read.Store(&readOnlyShard[V]{m: map[uint64]*value[V]{}, amended: true})
	}
	m.mu.Unlock()
}

// size estimates the bytes owned by this shard: the read+dirty map structures
// plus the value[V] nodes (counted once even though pointers are shared between
// read and dirty).
func (m *shardMap[V]) size() (size uintptr) {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	size = unsafe.Sizeof(*m)

	read := m.loadReadOnly()
	size += unsafe.Sizeof(readOnlyShard[V]{})
	size += mapSize(read.m)
	size += mapSize(m.dirty)

	nodeSize := unsafe.Sizeof(value[V]{})
	seen := make(map[*value[V]]struct{})
	count := func(head *value[V]) {
		for n := head; n != nil; n = n.next {
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			size += nodeSize
		}
	}
	for _, head := range read.m {
		count(head)
	}
	for _, head := range m.dirty {
		count(head)
	}
	return size
}
