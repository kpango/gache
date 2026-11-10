package gache

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/kpango/fastime"
)

// Expiry in gache is decided on the fastime clock (a 5ms ticker that is only
// re-synced with the wall clock every 100ms), not on time.Now(). Under -race
// on a loaded runner that clock can lag the wall clock by ~100ms and then jump
// forward, so a wall-clock time.Sleep around a TTL boundary is not a reliable
// proxy for "the cache considers this entry expired".
//
// Tests therefore follow two rules:
//   - "must be expired": wait on the cache clock (waitCacheElapsed /
//     waitCacheClockPast), never on time.Sleep.
//   - "must still be alive": keep the TTL at least 250ms beyond the point on
//     the cache clock where the assertion runs, which is larger than the clock
//     skew observed on loaded runners.

// cacheClockWaitTimeout bounds waitCacheClockPast so a stuck cache clock fails
// the test with a clear message instead of hanging until the package timeout.
const cacheClockWaitTimeout = 5 * time.Second

// cacheNow returns the clock gache uses to decide expiry.
func cacheNow() int64 {
	return fastime.UnixNanoNow()
}

// waitCacheClockPast blocks until the cache clock is strictly past deadline
// (UnixNano). An entry whose expire is <= deadline is expired afterwards.
func waitCacheClockPast(tb testing.TB, deadline int64) {
	tb.Helper()
	limit := time.Now().Add(cacheClockWaitTimeout)
	for cacheNow() <= deadline {
		if time.Now().After(limit) {
			tb.Fatalf("cache clock did not pass deadline within %v (lag=%v)",
				cacheClockWaitTimeout, time.Duration(deadline-cacheNow()))
		}
		time.Sleep(time.Millisecond)
	}
}

// waitCacheElapsed blocks until the cache clock has advanced strictly more than
// d since the call. Called right after SetWithExpire(k, v, d) (or any other
// call that sets an expire of at most d from now), the entry is expired when it
// returns, because the call-time cache clock is >= the clock used at Set.
func waitCacheElapsed(tb testing.TB, d time.Duration) {
	tb.Helper()
	waitCacheClockPast(tb, cacheNow()+int64(d))
}

// daemonWaitTimeout bounds every wait on the expiry daemon, so a stalled daemon
// fails the test instead of hanging until the package timeout. A healthy daemon
// finishes in milliseconds.
const daemonWaitTimeout = 15 * time.Second

// waitUntil polls cond every millisecond until it holds, failing the test after
// daemonWaitTimeout.
func waitUntil(tb testing.TB, what string, cond func() bool) {
	tb.Helper()
	limit := time.Now().Add(daemonWaitTimeout)
	for !cond() {
		if time.Now().After(limit) {
			tb.Fatalf("timed out after %v waiting for %s", daemonWaitTimeout, what)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitUntilProgress is waitUntil for a wait whose length scales with the work,
// which the race detector on a small CI runner stretches past daemonWaitTimeout:
// it fails only when progress() has not changed for daemonWaitTimeout, which is
// what a stall looks like.
func waitUntilProgress(tb testing.TB, what string, cond func() bool, progress func() int) {
	tb.Helper()
	last := progress()
	limit := time.Now().Add(daemonWaitTimeout)
	for !cond() {
		if p := progress(); p != last {
			last, limit = p, time.Now().Add(daemonWaitTimeout)
		} else if time.Now().After(limit) {
			tb.Fatalf("no progress for %v waiting for %s (progress %d)", daemonWaitTimeout, what, p)
		}
		time.Sleep(time.Millisecond)
	}
}

// newHookCache returns a cache whose expired hook is enabled, so every eviction
// of an expired entry lands on g.expChan (nothing drains it here).
func newHookCache() *gache[string] {
	return New[string](WithExpiredHookFunc[string](func(context.Context, string, string) {})).(*gache[string])
}

// evicted returns the entry sent to the expired hook channel, if any.
func evicted(g *gache[string]) (kv[string], bool) {
	select {
	case ev := <-g.expChan:
		return ev, true
	default:
		return kv[string]{}, false
	}
}

func stateOf[K comparable, V any](n *value[K, V]) uint64 { return n.state.Load() & stMask }

// newGacheValue is newValue for gache's node layout (with valPtr and expire).
func newGacheValue[K comparable, V any](k K, val V, expire int64) *value[K, V] {
	return newValue[K, V, atomic.Pointer[V], atomic.Int64](k, val, expire)
}

// promoted returns a shardMap[int, int] holding keys 0..n-1, each in a distinct
// bucket, all living in the read snapshot (not amended).
func promoted(n int) *shardMap[int, int] {
	sm := &shardMap[int, int]{}
	for i := range n {
		sm.store(uint64(i), i, i+100, 0)
	}
	sm.readSnapshot()
	return sm
}

// checkExpungeInvariant verifies, under m.mu, the invariant documented on shardMap:
// if dirty != nil every non-expunged node reachable from read is also reachable from
// dirty, no expunged node is reachable from dirty, and without a dirty map no
// expunged node is reachable from read.
func checkExpungeInvariant[K comparable, V any](t testing.TB, m *shardMap[K, V]) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if m.dirty == nil {
		for h, head := range read.m {
			for n := head; n != nil; n = loadNext(n) {
				if stateOf(n) == stExpunged {
					t.Errorf("bucket %#x: expunged node %v reachable from a read snapshot without dirty", h, n.key)
				}
			}
		}
		return
	}
	inDirty := map[*value[K, V]]struct{}{}
	for h, head := range m.dirty {
		for n := head; n != nil; n = loadNext(n) {
			inDirty[n] = struct{}{}
			if stateOf(n) == stExpunged {
				t.Errorf("bucket %#x: expunged node %v reachable from dirty", h, n.key)
			}
		}
	}
	for h, head := range read.m {
		for n := head; n != nil; n = loadNext(n) {
			if _, ok := inDirty[n]; !ok && stateOf(n) != stExpunged {
				t.Errorf("bucket %#x: non-expunged node %v reachable from read but not from dirty", h, n.key)
			}
		}
	}
}

type gcPayload struct {
	a, b uint64
	k    int
	pad  [40]byte
}

func (p *gcPayload) ok() bool { return p != nil && p.a == ^p.b }

// Valid reports whether the payload is intact (a is the complement of b).
func (p *gcPayload) Valid() bool { return p.ok() }

// NewGCPayload returns an intact payload for key k.
func NewGCPayload(a uint64, k int) *gcPayload { return &gcPayload{a: a, b: ^a, k: k} }

// gcStress runs the writers while runtime.GC() loops. A pointee the GC missed (its
// pointer was stored with a plain uint64 store, invisible to the write barrier) is
// detected twice over: its finalizer runs although the map still holds it, and an
// allocator reusing the freed memory with a pattern that fails ok() overwrites it.
func gcStress(t *testing.T, write func(k int, p *gcPayload), del func(k int), read func(k int) *gcPayload) {
	t.Helper()
	const keys = 16
	var (
		stop atomic.Bool
		bad  atomic.Int64
		wg   sync.WaitGroup
	)
	for w := range 4 {
		wg.Go(func() {
			for i := uint64(w); !stop.Load(); i += 4 {
				k := int(i) % keys
				if i%5 == 0 {
					del(k)
					continue
				}
				p := &gcPayload{a: i, b: ^i, k: k}
				runtime.SetFinalizer(p, func(p *gcPayload) {
					if read(p.k) == p { // unreachable for the GC, yet still stored
						bad.Add(1)
					}
				})
				write(k, p)
			}
		})
	}
	for range 3 {
		wg.Go(func() {
			for i := 0; !stop.Load(); i++ {
				if p := read(i % keys); p != nil && !p.ok() {
					bad.Add(1)
				}
			}
		})
	}
	wg.Go(func() {
		var sink *gcPayload
		for !stop.Load() {
			sink = &gcPayload{a: 1, b: 1}
		}
		runtime.KeepAlive(sink)
	})
	wg.Go(func() {
		for !stop.Load() {
			runtime.GC()
		}
	})
	time.Sleep(400 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
	if got := bad.Load(); got != 0 {
		t.Fatalf("observed %d reclaimed or corrupted payloads", got)
	}
}

// heapAlloc returns the live heap bytes after two full collections.
func heapAlloc() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// retained returns the heap bytes still held by what build returns, measured as
// the live-heap delta around it, together with its estimate from size.
func retained[T any](build func() T, size func(T) uintptr) (real, est int64) {
	before := heapAlloc()
	v := build()
	after := heapAlloc()
	est = int64(size(v))
	runtime.KeepAlive(v)
	return int64(after) - int64(before), est
}

// skipHeapMeasurement skips tests that compare Size with the live heap where the
// heap is not the one Size models: the race detector disables the tiny allocator
// and changes allocation sizes.
func skipHeapMeasurement(t *testing.T) {
	t.Helper()
	if raceDetectorEnabled {
		t.Skip("heap accounting differs under the race detector")
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("heap accounting is asserted for 64-bit platforms")
	}
}

func buildMap[V any](n int, state string, val func(int) V) *Map[int, V] {
	m := new(Map[int, V])
	for i := range n {
		m.Store(i, val(i))
	}
	switch state {
	case "dirty":
	case "read": // dirty promoted to the read snapshot
		m.Range(func(int, V) bool { return true })
	case "read+dirty": // a new key after a promotion rebuilds dirty next to read
		m.Range(func(int, V) bool { return true })
		m.Store(-1, val(0))
	case "half deleted":
		for i := 0; i < n; i += 2 {
			m.Delete(i)
		}
	}
	return m
}

// sameKeyChurnBudget bounds each same-key churn test (non-race wall time). A round
// is a short burst of contention followed by a quiescent consistency check.
const sameKeyChurnBudget = time.Second

// sameKeyRounds runs round until sameKeyChurnBudget has elapsed (at least once).
func sameKeyRounds(round func(n int)) {
	deadline := time.Now().Add(sameKeyChurnBudget)
	for n := 0; n == 0 || time.Now().Before(deadline); n++ {
		round(n)
	}
}

// sameKeyChurn runs 6 workers that Set/Delete the SAME few keys (so a fast-path
// revive races a delete of the very node it revives) plus one that keeps the
// shard amended with churnKeys fresh dirty-only keys (so deletes take the m.mu
// slow path and dirty rebuilds run), for one short burst.
func sameKeyChurn(churnKeys int, set, del func(k int, v int), churnSet, churnDel func(k, v int)) {
	const keys = 4
	var (
		stop atomic.Bool
		wg   sync.WaitGroup
	)
	for w := range 6 {
		wg.Go(func() {
			for i := 0; !stop.Load(); i++ {
				k := (i + w) % keys
				if (i/keys+w)%2 == 0 {
					set(k, i)
				} else {
					del(k, i)
				}
			}
		})
	}
	wg.Go(func() {
		for i := 0; !stop.Load(); i++ {
			k := 1000 + i%churnKeys
			churnSet(k, i)
			churnDel(k, i)
		}
	})
	time.Sleep(20 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

// stressBudget is the wall-clock length of one duration-bounded stress run; -short
// cuts it to a quarter. Tests that need a fixed number of rounds or goroutines
// (documented where they are) are not governed by it.
func stressBudget(d time.Duration) time.Duration {
	if testing.Short() {
		return d / 4
	}
	return d
}

// reviveABADuration is the minimum length of each churn run: long enough for a
// reader that accepts a payload from another incarnation to report bad hits.
// reviveABAMaxDuration caps a run that has not seen a hit yet: on a loaded CI
// runner (race detector, parallel tests) the readers can miss every live window
// of a 300ms run, which proves nothing either way.
const (
	reviveABADuration    = 300 * time.Millisecond
	reviveABAMaxDuration = 10 * time.Second
)

// runReviveChurn runs one writer that loops (revive, tombstone) of a single key
// and three lock-free readers for reviveABADuration, extended up to
// reviveABAMaxDuration until some read has seen the key. It returns how many
// reads reported the key present and how many of those carried the wrong value.
func runReviveChurn(set func(), del func(), read func() (ok, good bool)) (hits, bad int64) {
	var (
		stop         atomic.Bool
		hitN, badN   atomic.Int64
		writer, rdrs sync.WaitGroup
	)
	writer.Go(func() {
		for !stop.Load() {
			set()
			del()
		}
	})
	for range 3 {
		rdrs.Go(func() {
			for !stop.Load() {
				if ok, good := read(); ok {
					hitN.Add(1)
					if !good {
						badN.Add(1)
					}
				}
			}
		})
	}
	time.Sleep(reviveABADuration)
	for limit := time.Now().Add(reviveABAMaxDuration - reviveABADuration); hitN.Load() == 0 && time.Now().Before(limit); {
		time.Sleep(10 * time.Millisecond)
	}
	stop.Store(true)
	writer.Wait()
	rdrs.Wait()
	return hitN.Load(), badN.Load()
}

// Exported aliases for the black-box tests (package gache_test): files named
// *_test.go of package gache are compiled into the test binary, so the external
// test package reaches the clock and stress helpers above through these names.
var (
	WaitCacheElapsed   = waitCacheElapsed
	WaitCacheClockPast = waitCacheClockPast
	WaitUntil          = waitUntil
	WaitUntilProgress  = waitUntilProgress
	StressBudget       = stressBudget
	SameKeyRounds      = sameKeyRounds
	SameKeyChurn       = sameKeyChurn
	GCStress           = gcStress
	CacheNow           = cacheNow
)

// RaceDetectorEnabled reports whether the test binary is built with -race.
const RaceDetectorEnabled = raceDetectorEnabled

// GCPayload is the self-checking pointee type of GCStress.
type GCPayload = gcPayload

// RunReviveChurn is runReviveChurn.
func RunReviveChurn(set, del func(), read func() (ok, good bool)) (hits, bad int64) {
	return runReviveChurn(set, del, read)
}
