package gache

import (
	"context"
	"fmt"
	"hash/maphash"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/kpango/fastime"
)

func TestNode_StateEncoding(t *testing.T) {
	t.Parallel()

	sm := promoted(1)
	n, ok := sm.loadNode(0, 0)
	if !ok {
		t.Fatal("node for key 0 not found")
	}
	if got := n.state.Load(); got != stLive {
		t.Fatalf("fresh node state = %#x, want %#x", got, stLive)
	}
	c := sm.counter()
	n.mu.Lock()
	n.tombstone(c)
	s1 := n.state.Load()
	n.revive(7, 0, c)
	s2 := n.state.Load()
	n.tombstone(c)
	s3 := n.state.Load()
	n.mu.Unlock()
	if s1&stMask != stDeleted || s1>>3 != 1 {
		t.Fatalf("after tombstone state = %#x, want deleted generation 1", s1)
	}
	if s2&stMask != stLive || s2>>3 != 1 {
		t.Fatalf("after revive state = %#x, want live generation 1 (the tombstone's)", s2)
	}
	if s3&stMask != stDeleted || s3>>3 != 2 {
		t.Fatalf("after second tombstone state = %#x, want deleted generation 2", s3)
	}
	if s1 == s2 || s2 == s3 || s1 == s3 {
		t.Fatal("states of different incarnations must differ")
	}
	if got := c.Load(); got != 0 {
		t.Fatalf("length counter = %d after tombstone, revive, tombstone, want 0", got)
	}
}

func TestHasPointers_Kinds(t *testing.T) {
	t.Parallel()

	type (
		onePtr  struct{ p *int }
		nested  struct{ o onePtr }
		zeroPad struct {
			_ [0]int
			p *int
		}
		two     struct{ a, b int32 }
		bytes8  [8]byte
		ptrArr  [1]*int
		zeroArr [0]*int
	)
	tests := []struct {
		t    reflect.Type
		name string
		want bool
	}{
		{reflect.TypeFor[*int](), "pointer", true},
		{reflect.TypeFor[map[string]int](), "map", true},
		{reflect.TypeFor[chan int](), "chan", true},
		{reflect.TypeFor[func()](), "func", true},
		{reflect.TypeFor[unsafe.Pointer](), "unsafe.Pointer", true},
		{reflect.TypeFor[onePtr](), "struct{*int}", true},
		{reflect.TypeFor[nested](), "nested struct", true},
		{reflect.TypeFor[zeroPad](), "zero-size field before the pointer", true},
		{reflect.TypeFor[ptrArr](), "[1]*int", true},
		{reflect.TypeFor[int](), "int", false},
		{reflect.TypeFor[uint64](), "uint64", false},
		{reflect.TypeFor[float64](), "float64", false},
		{reflect.TypeFor[complex64](), "complex64", false},
		{reflect.TypeFor[uintptr](), "uintptr", false},
		{reflect.TypeFor[two](), "struct{int32,int32}", false},
		{reflect.TypeFor[bytes8](), "[8]byte", false},
		{reflect.TypeFor[zeroArr](), "[0]*int", false},
		{reflect.TypeFor[struct{}](), "struct{}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasPointers(tt.t); got != tt.want {
				t.Fatalf("hasPointers(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

// The pointer flag is decided when the node is created and decides which store
// primitive writes the payload word.
func TestNode_PointerFlag(t *testing.T) {
	t.Parallel()

	if got := newGacheValue("k", new(int), 0).state.Load() & stPtr; got == 0 {
		t.Fatal("node of a *int has no stPtr flag")
	}
	if got := newGacheValue("k", 5, 0).state.Load() & stPtr; got != 0 {
		t.Fatal("node of an int has the stPtr flag")
	}
	if got := newGacheValue("k", "s", 0).state.Load() & stPtr; got != 0 {
		t.Fatal("node of a string (read through valPtr) has the stPtr flag")
	}
	n := newGacheValue("k", "s", 0)
	if vp := n.vp().Load(); vp != &n.val || *vp != "s" {
		t.Fatalf("multi-word node valPtr = %p, want &val = %p", vp, &n.val)
	}
}

func TestNode_Sizes(t *testing.T) {
	t.Parallel()

	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("node layout is asserted for 64-bit platforms")
	}
	if got := unsafe.Sizeof(value[string, *int]{}); got != 64 {
		t.Fatalf("sizeof(value[string, *int]) = %d, want 64 (next, valPtr, key, val, expire, mu, state)", got)
	}
	if got := unsafe.Sizeof(value[string, int]{}); got != 64 {
		t.Fatalf("sizeof(value[string, int]) = %d, want 64", got)
	}
	// Map's node has no valPtr and no expire: 48 bytes (the 48-byte size class)
	// for a string key, 40 for an int key.
	if got := unsafe.Sizeof(valueOf[string, *int, struct{}, struct{}]{}); got != 48 {
		t.Fatalf("sizeof(Map node, string key) = %d, want 48 (next, key, val, mu, state)", got)
	}
	if got := unsafe.Sizeof(valueOf[int, *int, struct{}, struct{}]{}); got != 40 {
		t.Fatalf("sizeof(Map node, int key) = %d, want 40", got)
	}
}

// Delete then Store, LoadOrStore or Swap of a key living in the read snapshot is a
// revival under the node mutex only: it must complete while another goroutine
// holds the shard-wide m.mu.
func TestShardMap_ReviveInPlaceDoesNotTakeShardLock(t *testing.T) {
	t.Parallel()

	ops := []struct {
		do   func(t *testing.T, sm *shardMap[int, int], h uint64, k int)
		name string
	}{
		{name: "store", do: func(_ *testing.T, sm *shardMap[int, int], h uint64, k int) { sm.store(h, k, 1, 0) }},
		{name: "loadOrStore", do: func(t *testing.T, sm *shardMap[int, int], h uint64, k int) {
			if _, loaded := sm.loadOrStore(h, k, 2, 0); loaded {
				t.Error("loadOrStore of a deleted key reported loaded")
			}
		}},
		{name: "swap", do: func(t *testing.T, sm *shardMap[int, int], h uint64, k int) {
			if _, loaded := sm.upsert(h, k, 3, 0, true); loaded {
				t.Error("swap of a deleted key reported loaded")
			}
		}},
	}
	for _, amended := range []bool{false, true} {
		for _, op := range ops {
			t.Run(fmt.Sprintf("%s/amended=%v", op.name, amended), func(t *testing.T) {
				t.Parallel()
				sm := promoted(8)
				if amended {
					sm.store(100, 100, 0, 0) // amend: a dirty map now exists
				}
				if got := sm.loadReadOnly().amended; got != amended {
					t.Fatalf("amended = %v, want %v", got, amended)
				}
				n, _ := sm.loadNode(3, 3)
				lenBefore := sm.length()

				sm.mu.Lock()
				unlock := sync.OnceFunc(sm.mu.Unlock)
				done := make(chan struct{})
				// A Fatal below must not leave the worker blocked on m.mu, nor let it
				// report through t after the test has finished.
				t.Cleanup(func() { unlock(); <-done })
				go func() {
					defer close(done)
					for range 100 {
						if _, ok := sm.loadAndDelete(3, 3); !ok {
							t.Error("delete of a live key reported not found")
							return
						}
						op.do(t, sm, 3, 3)
					}
				}()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("Delete/Store of a read-resident key blocked on m.mu")
				}
				unlock()

				if _, _, ok := sm.loadValue(3, 3); !ok {
					t.Fatal("revived key is not live")
				}
				if got := sm.length(); got != lenBefore {
					t.Fatalf("length = %d, want %d", got, lenBefore)
				}
				if m, _ := sm.loadNode(3, 3); m != n {
					t.Fatal("revival replaced the node instead of reviving it in place")
				}
			})
		}
	}
}

// A deleted node skipped by a dirty rebuild is expunged and never revived: the
// writer falls to the slow path and supersedes it with a fresh node in dirty.
func TestShardMap_ExpungedNodeIsNeverRevived(t *testing.T) {
	t.Parallel()

	sm := promoted(4)
	old, _ := sm.loadNode(2, 2)
	if _, ok := sm.loadAndDelete(2, 2); !ok {
		t.Fatal("delete of key 2 reported not found")
	}
	if got := stateOf(old); got != stDeleted {
		t.Fatalf("state after delete = %d, want deleted (%d)", got, stDeleted)
	}

	sm.store(50, 50, 0, 0) // amend: dirtyLocked skips the deleted node and expunges it
	if got := stateOf(old); got != stExpunged {
		t.Fatalf("state after dirty rebuild = %d, want expunged (%d)", got, stExpunged)
	}
	checkExpungeInvariant(t, sm)

	if old.tryStore(99, 0, sm.counter()) {
		t.Fatal("tryStore revived an expunged node")
	}

	sm.store(2, 2, 222, 0)
	if v, _, ok := sm.loadValue(2, 2); !ok || v != 222 {
		t.Fatalf("loadValue(2) = %d, %v after storing over an expunged node, want 222 true", v, ok)
	}
	if n, _ := sm.loadNode(2, 2); n == old || n == nil {
		t.Fatal("expected a fresh node for the key")
	}
	if got := stateOf(old); got != stExpunged {
		t.Fatalf("old node state = %d, want it to stay expunged", got)
	}
	if got := sm.length(); got != 5 {
		t.Fatalf("length = %d, want 5", got)
	}
	checkExpungeInvariant(t, sm)

	// Promotion drops the expunged node for good.
	sm.readSnapshot()
	checkExpungeInvariant(t, sm)
	if v, _, ok := sm.loadValue(2, 2); !ok || v != 222 {
		t.Fatalf("loadValue(2) after promotion = %d, %v, want 222 true", v, ok)
	}
}

// A node revived before the rebuild reaches it is kept in dirty, not dropped.
func TestShardMap_DirtyRebuildKeepsRevivedNode(t *testing.T) {
	t.Parallel()

	sm := promoted(4)
	n, _ := sm.loadNode(1, 1)
	sm.loadAndDelete(1, 1)
	sm.store(1, 1, 111, 0) // revive in place before any rebuild
	sm.store(60, 60, 0, 0) // amend
	sm.mu.Lock()
	kept := sm.dirty[1] == n
	sm.mu.Unlock()
	if !kept {
		t.Fatal("revived node was dropped from the rebuilt dirty map")
	}
	if n.tryExpunge() {
		t.Fatal("tryExpunge expunged a live node")
	}
	checkExpungeInvariant(t, sm)
}

// Unique-key churn through the dirty map: the node unlinked by the delete is
// expunged, and the dirty map holds nothing but the stable nodes afterwards.
func TestShardMap_DirtyOnlyDeleteUnlinksAndExpunges(t *testing.T) {
	t.Parallel()

	sm := promoted(4)
	sm.store(70, 70, 1, 0) // amend; dirty-only node
	n, _ := sm.loadNode(70, 70)
	if _, ok := sm.loadAndDelete(70, 70); !ok {
		t.Fatal("delete of a dirty-only key reported not found")
	}
	if got := stateOf(n); got != stExpunged {
		t.Fatalf("unlinked dirty-only node state = %d, want expunged (%d)", got, stExpunged)
	}
	sm.mu.Lock()
	_, present := sm.dirty[70]
	sm.mu.Unlock()
	if present {
		t.Fatal("dirty-only node is still linked in dirty")
	}
	if got := sm.length(); got != 4 {
		t.Fatalf("length = %d, want 4", got)
	}
	checkExpungeInvariant(t, sm)
}

// Concurrent churn (revive in place, tombstone, overwrite, fresh insert) racing
// dirty rebuilds and promotions. Every worker owns a key range, so each result is
// checked against a private model; the expunge invariant is checked throughout and
// the length counter at the end. Run under -race it also covers the data races.
func TestShardMap_ExpungeInvariantUnderConcurrentChurn(t *testing.T) {
	t.Parallel()

	const (
		workers   = 6
		perWorker = 40
		buckets   = 8 // collision chains of ~30 nodes
	)
	sm := &shardMap[int, int]{}
	deadline := time.Now().Add(500 * time.Millisecond)

	type model struct {
		val     [perWorker]int
		present [perWorker]bool
	}
	models := make([]model, workers)
	var wg sync.WaitGroup
	var stop atomic.Bool

	for w := range workers {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 1))
			md := &models[w]
			for i := 0; !stop.Load(); i++ {
				idx := rng.IntN(perWorker)
				k := w*perWorker + idx
				h := uint64(k % buckets)
				v := i + 1
				switch rng.IntN(6) {
				case 0:
					sm.store(h, k, v, 0)
					md.val[idx], md.present[idx] = v, true
				case 1:
					got, loaded := sm.loadAndDelete(h, k)
					if loaded != md.present[idx] || (loaded && got != md.val[idx]) {
						t.Errorf("loadAndDelete(%d) = %d,%v, model %d,%v", k, got, loaded, md.val[idx], md.present[idx])
						return
					}
					md.present[idx] = false
				case 2:
					got, loaded := sm.loadOrStore(h, k, v, 0)
					if loaded != md.present[idx] || (loaded && got != md.val[idx]) || (!loaded && got != v) {
						t.Errorf("loadOrStore(%d) = %d,%v, model %d,%v", k, got, loaded, md.val[idx], md.present[idx])
						return
					}
					if !loaded {
						md.val[idx], md.present[idx] = v, true
					}
				case 3:
					prev, loaded := sm.upsert(h, k, v, 0, true)
					if loaded != md.present[idx] || (loaded && prev != md.val[idx]) {
						t.Errorf("swap(%d) = %d,%v, model %d,%v", k, prev, loaded, md.val[idx], md.present[idx])
						return
					}
					md.val[idx], md.present[idx] = v, true
				default:
					got, _, ok := sm.loadValue(h, k)
					if ok != md.present[idx] || (ok && got != md.val[idx]) {
						t.Errorf("loadValue(%d) = %d,%v, model %d,%v", k, got, ok, md.val[idx], md.present[idx])
						return
					}
				}
			}
		})
	}
	// Promoters: absent keys in busy buckets force misses, hence promotions; the next
	// insert of a fresh key then rebuilds dirty from a snapshot full of tombstones.
	for p := range 2 {
		wg.Go(func() {
			for i := 0; !stop.Load(); i++ {
				sm.loadValue(uint64(i%buckets), 1_000_000+p)
				if i%64 == 0 {
					sm.readSnapshot()
				}
			}
		})
	}
	wg.Go(func() {
		for !stop.Load() {
			checkExpungeInvariant(t, sm)
			runtime.Gosched()
		}
	})

	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stop.Store(true)
	wg.Wait()
	if t.Failed() {
		return
	}

	checkExpungeInvariant(t, sm)
	live := 0
	for w := range workers {
		for idx := range perWorker {
			k := w*perWorker + idx
			got, _, ok := sm.loadValue(uint64(k%buckets), k)
			if ok != models[w].present[idx] || (ok && got != models[w].val[idx]) {
				t.Fatalf("final key %d = %d,%v, model %d,%v", k, got, ok, models[w].val[idx], models[w].present[idx])
			}
			if ok {
				live++
			}
		}
	}
	if got := sm.length(); got != live {
		t.Fatalf("length counter = %d, live keys = %d", got, live)
	}
}

func TestShardMap_PointerValuesSurviveConcurrentGC(t *testing.T) {
	t.Parallel()

	var sm shardMap[int, *gcPayload]
	gcStress(t,
		func(k int, p *gcPayload) {
			if k%2 == 0 {
				sm.store(uint64(k), k, p, 0)
			} else {
				sm.upsert(uint64(k), k, p, 0, true)
			}
		},
		func(k int) { sm.loadAndDelete(uint64(k), k) },
		func(k int) *gcPayload { p, _, _ := sm.loadValue(uint64(k), k); return p },
	)
}

// A node found in a read snapshot that predates a Clear must adjust the counter
// of that snapshot, whether it is tombstoned or revived in place, so it can neither
// inflate nor deflate the length of the post-Clear epoch.
func TestShardMap_LengthCounterSurvivesClearChurn(t *testing.T) {
	t.Parallel()

	const keys, buckets = 32, 4
	sm := &shardMap[int, int]{}
	var (
		stop atomic.Bool
		wg   sync.WaitGroup
	)
	for w := range 4 {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 2))
			for i := 0; !stop.Load(); i++ {
				k := rng.IntN(keys)
				h := uint64(k % buckets)
				switch rng.IntN(5) {
				case 0:
					sm.store(h, k, i, 0)
				case 1:
					sm.loadAndDelete(h, k)
				case 2:
					sm.loadOrStore(h, k, i, 0)
				case 3:
					sm.upsert(h, k, i, 0, true)
				default:
					sm.loadValue(h, k)
				}
			}
		})
	}
	wg.Go(func() {
		for !stop.Load() {
			sm.clear()
			sm.readSnapshot()
			time.Sleep(100 * time.Microsecond)
		}
	})
	time.Sleep(400 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	live := 0
	sm.rangeShard(func(int, *value[int, int]) bool { live++; return true })
	if got := sm.length(); got != live {
		t.Fatalf("length counter = %d, live nodes = %d", got, live)
	}
	checkExpungeInvariant(t, sm)
}

// Set/Delete of a fresh key must not promote and rebuild the dirty map every time:
// with a handful of keys per shard a miss counted for the unlink would trip
// misses >= len(dirty) on each delete and cost a new dirty map plus snapshot per cycle.
func TestShardMap_DirtyOnlyChurnAllocatesOnlyTheNode(t *testing.T) {
	// Not parallel: testing.AllocsPerRun needs a quiet process.
	sm := promoted(4)
	sm.store(500, 500, 0, 0) // amend once
	sm.loadAndDelete(500, 500)
	allocs := testing.AllocsPerRun(200, func() {
		sm.store(500, 500, 1, 0)
		sm.loadAndDelete(500, 500)
	})
	if allocs > 1 {
		t.Fatalf("store+delete of a fresh key allocated %v times per cycle, want 1 (the node)", allocs)
	}
}

// TestShardMap_CollisionPrunedChainLostUpdate tests if an in-place store or delete
// to a live node in an interspersed collision chain is lost after dirtyLocked() promotes.
func TestShardMap_CollisionPrunedChainLostUpdate(t *testing.T) {
	m := &shardMap[string, string]{}
	h := uint64(0xcafe)

	// Store 3 colliding keys: k1, k2, k3
	m.store(h, "k1", "val1", -1)
	m.store(h, "k2", "val2", -1)
	m.store(h, "k3", "val3", -1)

	// Force promotion of dirty to read by reading a missing key len(dirty) times
	for range 10 {
		m.loadValue(0xbeef, "nonexistent")
	}

	// Verify all 3 keys are present in read snapshot
	if v, _, ok := m.loadValue(h, "k1"); !ok || v != "val1" {
		t.Fatalf("k1 missing or wrong value: %v, %v", v, ok)
	}
	if v, _, ok := m.loadValue(h, "k2"); !ok || v != "val2" {
		t.Fatalf("k2 missing or wrong value: %v, %v", v, ok)
	}
	if v, _, ok := m.loadValue(h, "k3"); !ok || v != "val3" {
		t.Fatalf("k3 missing or wrong value: %v, %v", v, ok)
	}

	// Delete k2 (middle node in chain k3 -> k2 -> k1)
	if _, ok := m.loadAndDelete(h, "k2"); !ok {
		t.Fatalf("failed to delete k2")
	}

	// Trigger dirtyLocked() by storing a new key under a different hash
	m.store(0x1234, "trigger", "val", -1)

	// Now dirty map has been rebuilt with a pruned copy-on-write chain for h:
	// liveNode(k3) -> liveNode(k1)
	// While read map still has old nodes: k3 -> k2(deleted) -> k1

	// In-place update k1
	m.store(h, "k1", "val1_updated", -1)

	// Now force promotion of dirty map to read
	for range 10 {
		m.loadValue(0xbeef, "nonexistent")
	}

	// Verify k1 value
	v, _, ok := m.loadValue(h, "k1")
	if !ok {
		t.Fatalf("k1 not found after promotion")
	}
	if v != "val1_updated" {
		t.Fatalf("k1 update was lost: got %q, want %q", v, "val1_updated")
	}
}

// TestShardMap_CollisionPrunedChainResurrection tests if deleting a node in the read map
// after dirtyLocked() results in silent resurrection of the deleted key when dirty is promoted.
func TestShardMap_CollisionPrunedChainResurrection(t *testing.T) {
	m := &shardMap[string, string]{}
	h := uint64(0xcafe)

	// Store 3 colliding keys: k1, k2, k3
	m.store(h, "k1", "val1", -1)
	m.store(h, "k2", "val2", -1)
	m.store(h, "k3", "val3", -1)

	// Promote dirty to read
	for range 10 {
		m.loadValue(0xbeef, "nonexistent")
	}

	// Delete k2 to make chain interspersed with dead node
	m.loadAndDelete(h, "k2")

	// Trigger dirtyLocked()
	m.store(0x1234, "trigger", "val", -1)

	// Now delete k1 from read map
	if _, ok := m.loadAndDelete(h, "k1"); !ok {
		t.Fatalf("failed to delete k1")
	}

	// Verify k1 is reported deleted
	if _, _, ok := m.loadValue(h, "k1"); ok {
		t.Fatalf("k1 should be deleted before promotion")
	}

	// Force promotion of dirty map to read
	for range 10 {
		m.loadValue(0xbeef, "nonexistent")
	}

	// Check if k1 resurrected!
	if v, _, ok := m.loadValue(h, "k1"); ok {
		t.Fatalf("k1 resurrected after promotion: got %q, ok=true", v)
	}
}

// TestShardMap_RaceStress100Goroutines stresses store, popIf, loadAndDelete (deleteKey),
// loadValue, and clear simultaneously across 100+ goroutines for a fixed number of
// iterations; with -race it detects unsynchronized access.
func TestShardMap_RaceStress100Goroutines(t *testing.T) {
	m := &shardMap[string, int]{}
	const goroutines = 120
	const numKeys = 20
	const iterations = 500

	keys := make([]string, numKeys)
	hashes := make([]uint64, numKeys)
	for i := range numKeys {
		keys[i] = fmt.Sprintf("race_key_%d", i)
		hashes[i] = uint64(i % 5) // Deliberate collisions: 20 keys over 5 hashes
	}

	var wg sync.WaitGroup
	var stop atomic.Bool

	for gID := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for iter := 0; iter < iterations && !stop.Load(); iter++ {
				idx := (id + iter) % numKeys
				k := keys[idx]
				h := hashes[idx]
				op := (id + iter) % 6

				switch op {
				case 0, 1: // store
					m.store(h, k, id*1000+iter, -1)
				case 2: // loadValue
					m.loadValue(h, k)
				case 3: // popIf
					m.popIf(h, k, nil)
				case 4: // loadAndDelete
					m.loadAndDelete(h, k)
				case 5: // clear (only some goroutines)
					if id%15 == 0 {
						m.clear()
					} else {
						m.loadValue(h, k)
					}
				}
			}
		}(gID)
	}

	wg.Wait()
}

// buildCollisionShard returns a shardMap[string, int] pre-seeded with `n` entries
// that all share the same uint64 hash h, exercising the collision chain.
// We bypass the gache layer and test shardMap directly.
func buildCollisionShard(h uint64, keys []string) *shardMap[string, int] {
	m := &shardMap[string, int]{}
	for i, k := range keys {
		m.store(h, k, i+1, -1)
	}
	return m
}

func TestShardMap_CollisionStoreLoad(t *testing.T) {
	t.Parallel()
	h := uint64(0xdeadbeef) // arbitrary; all keys share this h
	keys := []string{"alpha", "beta", "gamma", "delta"}
	m := buildCollisionShard(h, keys)

	for i, k := range keys {
		val, _, ok := m.loadValue(h, k)
		if !ok || val != i+1 {
			t.Errorf("key=%s: got (%d, %v), want (%d, true)", k, val, ok, i+1)
		}
	}
}

func TestShardMap_CollisionDeleteAndStore(t *testing.T) {
	t.Parallel()
	h := uint64(0xdeadbeef)
	keys := make([]string, 5)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	m := buildCollisionShard(h, keys)

	// Delete "k2"
	_, deleted := m.loadAndDelete(h, "k2")
	if !deleted {
		t.Fatal("loadAndDelete k2 failed")
	}
	if _, _, ok := m.loadValue(h, "k2"); ok {
		t.Error("deleted key still present")
	}

	// Revive k2 via store (tombstone revive)
	m.store(h, "k2", 99, -1)
	if val, _, ok := m.loadValue(h, "k2"); !ok || val != 99 {
		t.Errorf("revived key: got (%d, %v), want (99, true)", val, ok)
	}

	// Other colliding keys must still be intact
	for _, i := range []int{0, 1, 3, 4} {
		k := fmt.Sprintf("k%d", i)
		if val, _, ok := m.loadValue(h, k); !ok || val != i+1 {
			t.Errorf("key %s: got (%d, %v), want (%d, true)", k, val, ok, i+1)
		}
	}
}

func TestShardMap_CollisionConcurrent(t *testing.T) {
	t.Parallel()
	h := uint64(0xdeadbeef)
	m := buildCollisionShard(h, []string{"a", "b", "c", "d", "e"})

	const goroutines = 32
	const opsEach = 500
	keys := []string{"a", "b", "c", "d", "e"}

	var wg sync.WaitGroup
	for id := range goroutines {
		wg.Go(func() {
			for i := range opsEach {
				k := keys[(id+i)%len(keys)]
				switch i % 3 {
				case 0:
					m.store(h, k, id*opsEach+i, -1)
				case 1:
					m.loadValue(h, k)
				case 2:
					m.loadAndDelete(h, k)
				}
			}
		})
	}
	wg.Wait()
}

func TestShardMap_CollisionLenConsistency(t *testing.T) {
	t.Parallel()
	h := uint64(0xdeadbeef)
	const nkeys = 10

	keys := make([]string, nkeys)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}

	m := buildCollisionShard(h, keys)
	if l := m.length(); l != nkeys {
		t.Fatalf("after insert: length=%d, want %d", l, nkeys)
	}

	// Delete half
	for i := range nkeys / 2 {
		m.loadAndDelete(h, fmt.Sprintf("key%d", i))
	}
	if l := m.length(); l != nkeys/2 {
		t.Fatalf("after delete: length=%d, want %d", l, nkeys/2)
	}

	// Revive all (store on tombstoned keys)
	for i := range nkeys {
		m.store(h, fmt.Sprintf("key%d", i), i*10, -1)
	}
	if l := m.length(); l != nkeys {
		t.Fatalf("after revive: length=%d, want %d", l, nkeys)
	}
}

// Deleting a key that lives only in the dirty map must unlink it there, for gache
// as well as for Map: churning unique keys must not pile tombstones up in dirty
// until the next promotion.
func TestShardMap_DirtyOnlyDeleteDoesNotLeakTombstones(t *testing.T) {
	t.Parallel()

	const stable, churn = 100, 20000

	g := New[int]()
	for i := range stable {
		g.Set(fmt.Sprintf("stable-%d", i), i)
	}
	// Range promotes every shard: the stable keys now live in the read snapshots.
	g.Range(t.Context(), func(string, int, int64) bool { return true })

	for i := range churn {
		k := fmt.Sprintf("churn-%d", i)
		g.Set(k, i)
		if _, ok := g.Delete(k); !ok {
			t.Fatalf("Delete(%q) reported not found", k)
		}
	}
	if got := g.Len(); got != stable {
		t.Fatalf("Len = %d, want %d", got, stable)
	}

	buckets := 0
	for _, shard := range g.(*gache[int]).shards {
		shard.mu.Lock()
		buckets += len(shard.dirty)
		shard.mu.Unlock()
	}
	if buckets > stable {
		t.Fatalf("dirty maps hold %d buckets after %d set/delete cycles, want <= %d", buckets, churn, stable)
	}
}

// awaitNot fails if ch delivers within d: the caller is expected to still be
// waiting for n.mu.
func awaitNot[T any](t *testing.T, ch <-chan T, d time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("returned %v while n.mu was held and the node unchanged", v)
	case <-time.After(d):
	}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("did not return")
		panic("unreachable")
	}
}

// TestShardMap_ReviveToOwnValueDoesNotAllocate pins that reviving a node with the value it
// was created with points valPtr back at the node's own val instead of copying it.
func TestShardMap_ReviveToOwnValueDoesNotAllocate(t *testing.T) {
	// Not parallel: testing.AllocsPerRun needs a quiet process.
	if raceDetectorEnabled {
		t.Skip("allocation counts differ under the race detector")
	}
	v, other := strings.Repeat("v", 8), strings.Repeat("w", 8)
	sm := &shardMap[string, string]{}
	sm.store(1, "k", v, 0)
	sm.readSnapshot()
	n := findNode(sm.loadReadOnly().m[1], "k")

	allocs := testing.AllocsPerRun(100, func() {
		sm.loadAndDelete(1, "k")
		sm.store(1, "k", v, 0)
	})
	if allocs != 0 {
		t.Errorf("delete+store of the node's own value allocated %v times, want 0", allocs)
	}
	if got := n.vp().Load(); got != &n.val {
		t.Errorf("valPtr = %p after revival with the node's own value, want &n.val %p", got, &n.val)
	}
	if got, _, ok := sm.loadValue(1, "k"); !ok || got != v {
		t.Errorf("loadValue = (%q, %v), want (%q, true)", got, ok, v)
	}

	allocs = testing.AllocsPerRun(100, func() {
		sm.loadAndDelete(1, "k")
		sm.store(1, "k", other, 0)
	})
	if allocs != 1 {
		t.Errorf("delete+store of another value allocated %v times, want 1 (the copy)", allocs)
	}
	if got, _, ok := sm.loadValue(1, "k"); !ok || got != other {
		t.Errorf("loadValue = (%q, %v), want (%q, true)", got, ok, other)
	}
}

// tombstone stores the deleted state before it clears the payload, so a store racing a
// delete can find a node that is not live while valPtr still points at the old value.
// Storing that same value must revive the node, not only refresh its expire: the
// lock-free same-value path requires a live node.
func TestShardMap_StoreSameValueRevivesNodeBeingTombstoned(t *testing.T) {
	t.Parallel()

	v := strings.Repeat("v", 8)
	sm := &shardMap[string, string]{}
	sm.store(1, "k", v, 0)
	sm.readSnapshot()
	n := findNode(sm.loadReadOnly().m[1], "k")
	n.state.Store(n.state.Load()&^stMask | stDeleted) // the window: state moved, valPtr not yet cleared
	if n.vp().Load() == nil {
		t.Fatal("test setup: valPtr cleared")
	}

	sm.store(1, "k", v, 7)
	if got, exp, ok := sm.loadValue(1, "k"); !ok || got != v || exp != 7 {
		t.Fatalf("loadValue after storing the same value = (%q, %d, %v), want (%q, 7, true)", got, exp, ok, v)
	}
}

// TestShardMap_TryStorePollsDeadNode pins how a same-value store waits for a two-word
// node's lock: while the node is dead it does not queue on n.mu, and it returns
// through the lock-free path once the lock holder revives the node with that
// value; a live node with another value is waited for under n.mu as before.
func TestShardMap_TryStorePollsDeadNode(t *testing.T) {
	t.Parallel()
	const v = "v"

	t.Run("RevivedWithSameValue", func(t *testing.T) {
		t.Parallel()
		c := new(atomic.Int64)
		n := newGacheValue("k", v, 1)
		n.mu.Lock()
		unlock := sync.OnceFunc(n.mu.Unlock)
		t.Cleanup(unlock) // a failed await must not leave the store blocked on n.mu
		n.tombstone(c)
		done := make(chan bool, 1)
		go func() { done <- n.tryStore(v, 5, c) }()
		awaitNot(t, done, 20*time.Millisecond)
		n.revive(v, 7, c) // still holding n.mu
		if ok := await(t, done); !ok {
			t.Error("tryStore = false, want true")
		}
		if got := n.loadExpire(); got != 5 {
			t.Errorf("expire = %d, want 5 (the polling store's)", got)
		}
		unlock()
	})

	t.Run("LiveWithOtherValue", func(t *testing.T) {
		t.Parallel()
		c := new(atomic.Int64)
		n := newGacheValue("k", "w", 1)
		n.mu.Lock()
		unlock := sync.OnceFunc(n.mu.Unlock)
		t.Cleanup(unlock) // a failed await must not leave the store blocked on n.mu
		done := make(chan bool, 1)
		go func() { done <- n.tryStore(v, 5, c) }()
		awaitNot(t, done, 20*time.Millisecond)
		unlock()
		if ok := await(t, done); !ok {
			t.Error("tryStore = false, want true")
		}
		if got, e, ok := n.readValue(); !ok || got != v || e != 5 {
			t.Errorf("readValue = (%q, %d, %v), want (%q, 5, true)", got, e, ok, v)
		}
	})
}

// TestShardMap_TombstoneIfExpiredPollsLockedNode pins that an expiry eviction finding
// n.mu held does not queue on it: it leaves with the result it would get under
// the lock as soon as the node is dead or fresh, and tombstones the node itself
// once the lock is released with the node still expired.
func TestShardMap_TombstoneIfExpiredPollsLockedNode(t *testing.T) {
	t.Parallel()
	type result struct {
		val          string
		loaded, live bool
	}
	tests := []struct {
		holder func(n *value[string, string], c *atomic.Int64) // runs under n.mu
		name   string
		want   result
		unlock bool // the result arrives only after n.mu is released
	}{
		{name: "TombstonedByHolder", holder: func(n *value[string, string], c *atomic.Int64) { n.tombstone(c) }, want: result{}},
		{name: "RefreshedByHolder", holder: func(n *value[string, string], _ *atomic.Int64) { n.storeExpire(1 << 62) }, want: result{live: true}},
		{name: "ReleasedUnchanged", holder: func(*value[string, string], *atomic.Int64) {}, want: result{val: "v", loaded: true, live: true}, unlock: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := new(atomic.Int64)
			c.Store(1)
			n := newGacheValue("k", "v", 1) // expired at now = 100
			n.mu.Lock()
			unlock := sync.OnceFunc(n.mu.Unlock)
			t.Cleanup(unlock) // a failed await must not leave the eviction blocked on n.mu
			done := make(chan result, 1)
			go func() {
				var r result
				r.val, r.loaded, r.live = n.tombstoneIfExpired(c, 100, false)
				done <- r
			}()
			awaitNot(t, done, 20*time.Millisecond)
			tt.holder(n, c)
			if tt.unlock {
				awaitNot(t, done, 20*time.Millisecond)
				unlock()
			}
			if got := await(t, done); got != tt.want {
				t.Errorf("tombstoneIfExpired = %+v, want %+v", got, tt.want)
			}
			if !tt.unlock {
				unlock()
			}
			if want := tt.want.live && !tt.want.loaded; n.live() != want {
				t.Errorf("node live = %v, want %v", n.live(), want)
			}
		})
	}
}

// popSlow must never unlink a node a fast path can still revive. Several goroutines
// Set/Delete the same keys while another keeps the shard amended with fresh keys,
// so a delete that takes the slow path can meet a node that is shared with the
// read snapshot and revived concurrently. At quiescence the expunge invariant must
// hold and the length counter must equal the live nodes seen after promotion
// (a node unlinked from dirty while still revivable loses its next Set and leaves
// the counter one too high).
func TestShardMap_SameKeyChurnKeepsInvariantAndLength(t *testing.T) {
	t.Parallel()

	const keys = 4
	sameKeyRounds(func(n int) {
		sm := &shardMap[int, int]{}
		for k := range keys {
			sm.store(uint64(k), k, k, 0)
		}
		sm.readSnapshot()
		sameKeyChurn(16,
			func(k, v int) { sm.store(uint64(k), k, v, 0) },
			func(k, _ int) { sm.loadAndDelete(uint64(k), k) },
			func(k, v int) { sm.store(uint64(k), k, v, 0) },
			func(k, _ int) { sm.loadAndDelete(uint64(k), k) },
		)

		checkExpungeInvariant(t, sm)
		sm.readSnapshot()
		live := 0
		sm.rangeShard(func(int, *value[int, int]) bool { live++; return true })
		if got := sm.length(); got != live {
			t.Fatalf("round %d: length counter = %d, live nodes after promotion = %d", n, got, live)
		}
		if t.Failed() {
			t.Fatalf("round %d: expunge invariant violated", n)
		}
	})
}

// TestShardMap_HighChurnHashCollisions inserts, updates and deletes 100,000 keys
// across 100 fixed hashes (collision chains of ~1,000 nodes) and checks that a
// dirty rebuild plus promotion prunes every tombstone from the read chains and
// that the surviving keys keep their latest values.
func TestShardMap_HighChurnHashCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 100k churn stress test in short mode")
	}

	m := &shardMap[string, int]{}
	const numHashes = 100
	const totalKeys = 100_000

	// Step 1: Insert 100,000 keys into 100 hash buckets (~1,000 keys per collision chain)
	t.Logf("Step 1: Inserting %d keys across %d hash buckets...", totalKeys, numHashes)
	for i := range totalKeys {
		h := uint64(i % numHashes)
		k := fmt.Sprintf("key_%d", i)
		m.store(h, k, i, -1)
	}

	if l := m.length(); l != totalKeys {
		t.Fatalf("length after insert: got %d, want %d", l, totalKeys)
	}

	// Step 2: Mutate (update) half of the keys
	t.Logf("Step 2: Mutating %d keys...", totalKeys/2)
	for i := range totalKeys / 2 {
		h := uint64(i % numHashes)
		k := fmt.Sprintf("key_%d", i)
		m.store(h, k, i*10, -1)
	}

	// Step 3: Delete 90,000 keys (leaving 10,000 live keys)
	t.Logf("Step 3: Deleting 90,000 keys...")
	deletedCount := 0
	for i := range 90_000 {
		h := uint64(i % numHashes)
		k := fmt.Sprintf("key_%d", i)
		if _, ok := m.loadAndDelete(h, k); ok {
			deletedCount++
		}
	}
	if deletedCount != 90_000 {
		t.Fatalf("deleted count mismatch: got %d, want 90000", deletedCount)
	}

	if l := m.length(); l != 10_000 {
		t.Fatalf("length after 90k deletes: got %d, want 10000", l)
	}

	// Step 4: Force promotions and dirty map rebuilds
	t.Logf("Step 4: Forcing dirty rebuild and promotion...")
	for i := range 200 {
		m.store(uint64(1000+i), fmt.Sprintf("extra_%d", i), 1, -1)
		for range 5 {
			m.loadValue(0xdeadbeef, "missing")
		}
	}

	// Run a GC before inspecting the chains (no memory is measured)
	runtime.GC()
	runtime.GC()

	// Step 5: Check chain lengths and tombstoned node accumulation
	t.Logf("Step 5: Inspecting read snapshot chains for dead node accumulation...")
	read := m.loadReadOnly()
	totalTombstones := 0
	totalLive := 0
	for h := range uint64(numHashes) {
		head := read.m[h]
		chainLen := 0
		for n := head; n != nil; n = n.next {
			chainLen++
			if !n.live() {
				totalTombstones++
			} else {
				totalLive++
			}
		}
	}
	t.Logf("After pruning: live nodes in test hashes=%d, tombstone nodes remaining=%d", totalLive, totalTombstones)

	if totalTombstones > 0 {
		t.Errorf("%d tombstoned nodes still retained in promoted read chains", totalTombstones)
	}
	if totalLive != 10_000 {
		t.Errorf("found %d live nodes, want 10000", totalLive)
	}

	// Step 6: Verify value integrity of remaining live keys
	t.Logf("Step 6: Verifying value integrity of remaining live keys...")
	lostUpdates := 0
	for i := 90_000; i < totalKeys; i++ {
		h := uint64(i % numHashes)
		k := fmt.Sprintf("key_%d", i)
		val, _, ok := m.loadValue(h, k)
		if !ok {
			t.Errorf("live key missing: %s", k)
			continue
		}
		expected := i
		if i < totalKeys/2 {
			expected = i * 10
		}
		if val != expected {
			lostUpdates++
		}
	}
	if lostUpdates > 0 {
		t.Fatalf("%d keys lost an update or hold a corrupted value", lostUpdates)
	}
}

// TestShardMap_CollisionResurrectionAndLostUpdateSequence replays one sequence
// (single goroutine): deleting or updating a node of a collision chain that has a
// pruned tombstone in it must not resurrect a deleted node or lose a live update
// when the dirty map is promoted.
func TestShardMap_CollisionResurrectionAndLostUpdateSequence(t *testing.T) {
	m := &shardMap[string, string]{}
	h := uint64(42)

	// Chain: C -> B -> A
	m.store(h, "A", "valA", -1)
	m.store(h, "B", "valB", -1)
	m.store(h, "C", "valC", -1)

	// Promote dirty to read
	for range 10 {
		m.loadValue(0x999, "miss")
	}

	// Delete B (middle node)
	if _, ok := m.loadAndDelete(h, "B"); !ok {
		t.Fatal("failed to delete B")
	}

	// Trigger dirtyLocked: this prunes B and creates new copies of C and A in dirty!
	m.store(uint64(100), "other", "val", -1)

	// Update A while dirty is active
	m.store(h, "A", "valA_updated", -1)

	// Also delete C while dirty is active
	if _, ok := m.loadAndDelete(h, "C"); !ok {
		t.Fatal("failed to delete C")
	}

	// Promote dirty to read
	for range 10 {
		m.loadValue(0x999, "miss")
	}

	// Check A: did it keep valA_updated?
	valA, _, okA := m.loadValue(h, "A")
	if !okA {
		t.Fatal("A is missing after promotion")
	}
	if valA != "valA_updated" {
		t.Errorf("lost update on A: got %q, want %q", valA, "valA_updated")
	}

	// Check C: is it still deleted, or did it resurrect?
	valC, _, okC := m.loadValue(h, "C")
	if okC {
		t.Errorf("deleted node C resurrected after dirty promotion: got %q, want deleted", valC)
	}

	// Check Len: only A and "other" are live
	expectedLen := 2 // "A" and "other"
	if l := m.length(); l != expectedLen {
		t.Errorf("shard length mismatch: got %d, want %d", l, expectedLen)
	}
}

// expireIf is the shardMap primitive behind expiration. now is 1000 in all cases:
// an entry is expired when 0 < expire < now. Every stored key shares the hash h, so
// they all sit in one collision chain.
func TestShardMap_ExpireIf(t *testing.T) {
	t.Parallel()

	const now, h = 1000, 7
	tests := []struct {
		stored  map[string]int64 // key -> expire
		name    string
		key     string
		want    bool // the entry for key is evicted
		promote bool
	}{
		{name: "expired", key: "k", stored: map[string]int64{"k": now - 1}, want: true},
		{name: "expired, promoted", key: "k", stored: map[string]int64{"k": now - 1}, want: true, promote: true},
		{name: "expires exactly now is alive", key: "k", stored: map[string]int64{"k": now}},
		{name: "expires later", key: "k", stored: map[string]int64{"k": now + 1}},
		{name: "expires later, promoted", key: "k", stored: map[string]int64{"k": now + 1}, promote: true},
		{name: "no TTL (0)", key: "k", stored: map[string]int64{"k": 0}},
		{name: "no TTL (-1), promoted", key: "k", stored: map[string]int64{"k": -1}, promote: true},
		{name: "absent key", key: "missing", stored: map[string]int64{"k": now - 1}},
		{
			name: "chain: only the expired key goes", key: "k", want: true,
			stored: map[string]int64{"k": now - 1, "fresh": now + 1, "ttl-less": 0},
		},
		{
			name: "chain: fresh key survives its expired neighbours", key: "fresh", promote: true,
			stored: map[string]int64{"fresh": now + 1, "old1": now - 1, "old2": now - 5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := &shardMap[string, string]{}
			for k, e := range tt.stored {
				sm.store(h, k, "v-"+k, e)
			}
			if tt.promote {
				sm.readSnapshot()
			}
			before := sm.length()

			val, loaded := sm.expireIf(h, tt.key, now)
			if loaded != tt.want {
				t.Fatalf("expireIf loaded = %v, want %v", loaded, tt.want)
			}
			if tt.want && val != "v-"+tt.key {
				t.Errorf("expireIf value = %q, want %q", val, "v-"+tt.key)
			}
			wantLen := before
			if tt.want {
				wantLen--
			}
			if got := sm.length(); got != wantLen {
				t.Errorf("length = %d, want %d", got, wantLen)
			}
			for k := range tt.stored {
				_, _, ok := sm.loadValue(h, k)
				if want := !(tt.want && k == tt.key); ok != want {
					t.Errorf("key %q present = %v, want %v", k, ok, want)
				}
			}
		})
	}
}

// A dirty-only node that expireIf evicts is unlinked from dirty at once, as
// loadAndDelete does, instead of piling tombstones up until the next promotion.
func TestShardMap_ExpireIf_UnlinksDirtyOnlyNode(t *testing.T) {
	t.Parallel()

	const h = 3
	sm := &shardMap[string, string]{}
	sm.store(h, "a", "va", 1)
	sm.store(h+1, "b", "vb", 1)
	sm.store(h, "c", "vc", 0) // same chain as "a"

	if _, ok := sm.expireIf(h, "a", 2); !ok {
		t.Fatal("expireIf(a) evicted nothing")
	}
	if _, ok := sm.expireIf(h+1, "b", 2); !ok {
		t.Fatal("expireIf(b) evicted nothing")
	}

	sm.mu.Lock()
	buckets, chained := len(sm.dirty), sm.chained
	sm.mu.Unlock()
	if buckets != 1 || chained != 0 {
		t.Errorf("dirty holds %d buckets and %d chained nodes, want 1 and 0: evicted nodes were left in dirty", buckets, chained)
	}
	if got := sm.length(); got != 1 {
		t.Errorf("length = %d, want 1", got)
	}
}

// The amended-miss paths of loadAndDelete and expireIf share evictSlow and hand it
// a node step as a closure: the closure must stay on the stack, so a miss that
// takes the slow path allocates nothing.
func TestShardMap_EvictSlowNoAllocs(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("the race detector allocates")
	}

	// A dirty map larger than the number of runs below, so the misses never promote it.
	sm := &shardMap[int, int]{}
	for i := range 4000 {
		sm.store(uint64(i), i, i, 0)
	}
	if read := sm.loadReadOnly(); !read.amended {
		t.Fatal("the read snapshot is not amended: misses would not take the slow path")
	}

	const absent = 1 << 30
	if n := testing.AllocsPerRun(1000, func() { sm.popIf(absent, absent, nil) }); n != 0 {
		t.Errorf("popIf miss allocs = %v, want 0", n)
	}
	if n := testing.AllocsPerRun(1000, func() { sm.expireIf(absent, absent, 1000) }); n != 0 {
		t.Errorf("expireIf miss allocs = %v, want 0", n)
	}
	if read := sm.loadReadOnly(); !read.amended {
		t.Error("the misses promoted dirty: they did not all take the slow path")
	}
}

// TestShardMap_DeleteExpiredAfterClearDecrementsOwningCounter: a Clear that lands
// while deleteExpired is still tombstoning the nodes of its (pre-Clear) snapshot
// must not let those tombstones decrement the post-Clear epoch's counter: they
// belong to the old counter, so the length of the new epoch stays exactly the
// entries stored after the Clear.
func TestShardMap_DeleteExpiredAfterClearDecrementsOwningCounter(t *testing.T) {
	t.Parallel()

	sm := new(shardMap[string, int])
	now := fastime.UnixNanoNow()
	const expiredCount = 10
	for i := range expiredCount {
		sm.store(uint64(i), fmt.Sprintf("exp_key_%d", i), i, now-1000)
	}
	sm.readSnapshot() // promote: the sweep walks the read snapshot
	cOld := sm.counter()
	if l := sm.length(); l != expiredCount {
		t.Fatalf("initial length = %d, want %d", l, expiredCount)
	}

	// The first expired entry's hook runs after its tombstone and, like a
	// concurrent Clear plus writer, replaces the epoch before the sweep has
	// reached the other nine nodes of the old snapshot.
	var once sync.Once
	purged := sm.deleteExpired(now, func(string, int) {
		once.Do(func() {
			sm.clear()
			sm.store(9999, "new_live_key", 100, 0)
		})
	})
	if purged != expiredCount {
		t.Fatalf("deleteExpired purged %d entries, want %d", purged, expiredCount)
	}
	cNew := sm.counter()
	if cOld == cNew {
		t.Fatal("clear did not install a distinct epoch counter")
	}
	if got := cNew.Load(); got != 1 {
		t.Fatalf("post-Clear counter = %d, want 1: tombstones of the old snapshot hit the new epoch", got)
	}
	if l := sm.length(); l != 1 {
		t.Fatalf("length = %d, want 1 (new_live_key)", l)
	}
	if got := cOld.Load(); got != 0 {
		t.Fatalf("pre-Clear counter = %d, want 0 after all its nodes were tombstoned", got)
	}
	if val, _, ok := sm.loadValue(9999, "new_live_key"); !ok || val != 100 {
		t.Fatalf("new_live_key = (%d, %v), want (100, true)", val, ok)
	}
}

// TestShardMap_LengthClampsNegativeCounter: a reader can observe the length counter
// below zero while a decrement races the increment it follows; length reports 0
// then, never a negative Len.
func TestShardMap_LengthClampsNegativeCounter(t *testing.T) {
	t.Parallel()

	sm := new(shardMap[int, int])
	sm.counter().Add(-3)
	if got := sm.length(); got != 0 {
		t.Fatalf("length with the counter at -3 = %d, want 0", got)
	}
	sm.counter().Add(5)
	if got := sm.length(); got != 2 {
		t.Fatalf("length with the counter at 2 = %d, want 2", got)
	}
}

// TestShardMap_ClearRacesDeleteExpired: after writers of short-lived and permanent
// keys race Clear and DeleteExpired for a stress budget, an expiry sweep leaves Len
// equal to the entries Range visits, all of them permanent.
func TestShardMap_ClearRacesDeleteExpired(t *testing.T) {
	t.Parallel()

	gc := New[int](WithMaxWorkers[int](8))
	defer gc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), stressBudget(300*time.Millisecond))
	defer cancel()

	var wg sync.WaitGroup
	for w := range 4 { // short-lived keys (1ms TTL)
		wg.Go(func() {
			for i := 0; ctx.Err() == nil; i++ {
				gc.SetWithExpire(fmt.Sprintf("exp_%d_%d", w, i), i, time.Millisecond)
				if i%20 == 0 {
					time.Sleep(500 * time.Microsecond)
				}
			}
		})
	}
	for range 2 {
		wg.Go(func() {
			for ctx.Err() == nil {
				gc.DeleteExpired(ctx)
				time.Sleep(time.Millisecond)
			}
		})
	}
	wg.Go(func() {
		for ctx.Err() == nil {
			time.Sleep(5 * time.Millisecond)
			gc.Clear()
		}
	})
	for w := range 2 { // permanent keys
		wg.Go(func() {
			for i := 0; ctx.Err() == nil; i++ {
				gc.SetWithExpire(fmt.Sprintf("live_%d_%d", w, i), i, NoTTL)
				if i%50 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		})
	}
	wg.Wait()

	waitCacheElapsed(t, time.Millisecond) // every short-lived key is now expired
	gc.DeleteExpired(context.Background())
	var mu sync.Mutex // Range calls f from several goroutines
	ranged := 0
	gc.Range(context.Background(), func(k string, _ int, _ int64) bool {
		mu.Lock()
		ranged++
		mu.Unlock()
		if !strings.HasPrefix(k, "live_") {
			t.Errorf("key %q survived the expiry sweep", k)
		}
		return true
	})
	if l := gc.Len(); l != ranged {
		t.Fatalf("Len = %d, Range visited %d entries", l, ranged)
	}
}

// TestShardMap_DeepCollisionChainTombstonePrune tests a deep collision chain
// of 200 keys on the exact same uint64 hash bucket with alternating updates, deletes,
// promotions, and checks for zero lost updates, zero resurrections, and exact Len().
func TestShardMap_DeepCollisionChainTombstonePrune(t *testing.T) {
	m := &shardMap[string, string]{}
	const h = uint64(0xfeedfacecafe)
	const chainDepth = 200

	keys := make([]string, chainDepth)
	for i := range chainDepth {
		keys[i] = fmt.Sprintf("deep_chain_key_%04d", i)
		m.store(h, keys[i], fmt.Sprintf("init_%d", i), -1)
	}

	if l := m.length(); l != chainDepth {
		t.Fatalf("length after inserting %d keys = %d", chainDepth, l)
	}

	// Promote dirty to read
	for range 250 {
		m.loadValue(0x1111, "miss")
	}

	// Delete every even key
	for i := 0; i < chainDepth; i += 2 {
		if _, ok := m.loadAndDelete(h, keys[i]); !ok {
			t.Fatalf("failed to delete key %s", keys[i])
		}
	}

	if l := m.length(); l != chainDepth/2 {
		t.Fatalf("length after deleting %d keys = %d, want %d", chainDepth/2, l, chainDepth/2)
	}

	// Trigger dirtyLocked() by storing on a different hash
	m.store(0x9999, "trigger_key", "trigger_val", -1)

	// In-place update all surviving odd keys
	for i := 1; i < chainDepth; i += 2 {
		m.store(h, keys[i], fmt.Sprintf("updated_%d", i), -1)
	}

	// Promote dirty to read
	for range 250 {
		m.loadValue(0x1111, "miss")
	}

	// Trigger GC
	runtime.GC()

	// Verify all odd keys have updated values and no even keys resurrected
	for i := range chainDepth {
		val, _, ok := m.loadValue(h, keys[i])
		if i%2 == 0 {
			if ok {
				t.Fatalf("deleted key %s resurrected: val = %q", keys[i], val)
			}
		} else {
			if !ok {
				t.Fatalf("live key %s missing", keys[i])
			}
			expected := fmt.Sprintf("updated_%d", i)
			if val != expected {
				t.Fatalf("live key %s lost an update: got %q, want %q", keys[i], val, expected)
			}
		}
	}

	// Verify length (100 odd keys + 1 trigger_key)
	expectedLen := chainDepth/2 + 1
	if l := m.length(); l != expectedLen {
		t.Fatalf("length mismatch: got %d, want %d", l, expectedLen)
	}
}

// TestShardMap_ArbitraryUnlinkPatterns tests arbitrary combinations of dead node patterns:
// leading dead, trailing dead, consecutive dead, alternating dead, all dead, all live.
func TestShardMap_ArbitraryUnlinkPatterns(t *testing.T) {
	patterns := []struct {
		name    string
		pattern []bool // true = delete, false = keep
	}{
		{"AllLive", []bool{false, false, false, false, false}},
		{"AllDead", []bool{true, true, true, true, true}},
		{"LeadingDead", []bool{true, true, false, false, false}},
		{"TrailingDead", []bool{false, false, false, true, true}},
		{"AlternatingDead1", []bool{true, false, true, false, true}},
		{"AlternatingDead2", []bool{false, true, false, true, false}},
		{"ConsecutiveMiddleDead", []bool{false, true, true, true, false}},
		{"SingleLiveMiddle", []bool{true, true, false, true, true}},
	}

	for _, tc := range patterns {
		t.Run(tc.name, func(t *testing.T) {
			m := &shardMap[string, int]{}
			h := uint64(12345)
			n := len(tc.pattern)

			// Store keys in reverse so head order matches pattern index
			for i := n - 1; i >= 0; i-- {
				m.store(h, fmt.Sprintf("k%d", i), i, -1)
			}

			// Promote to read
			for range n * 2 {
				m.loadValue(0x9999, "miss")
			}

			// Apply deletions according to pattern
			liveCount := 0
			for i, shouldDel := range tc.pattern {
				if shouldDel {
					m.loadAndDelete(h, fmt.Sprintf("k%d", i))
				} else {
					liveCount++
				}
			}

			// Rebuild dirty (this triggers pruning)
			m.store(0x9999, "trigger", 999, -1)

			// Promote dirty to read
			for range n * 2 {
				m.loadValue(0x9999, "miss")
			}

			// Verify live keys exist, deleted keys do not
			for i, shouldDel := range tc.pattern {
				v, _, ok := m.loadValue(h, fmt.Sprintf("k%d", i))
				if shouldDel {
					if ok {
						t.Fatalf("Pattern %s: Deleted key k%d resurrected with value %d", tc.name, i, v)
					}
				} else {
					if !ok || v != i {
						t.Fatalf("Pattern %s: Live key k%d missing or wrong: got (%d, %v), want %d", tc.name, i, v, ok, i)
					}
				}
			}

			// Check length (live keys + 1 trigger)
			expectedLen := liveCount + 1
			if l := m.length(); l != expectedLen {
				t.Fatalf("Pattern %s: length mismatch: got %d, want %d", tc.name, l, expectedLen)
			}
		})
	}
}

// TestShardMap_DeleteExpiredCollisionChain directly tests shardMap.deleteExpired
// on synthetic multi-node collision chains to verify in-shard traversal, tombstoning,
// counter decrementing, and exact callback dispatch.
func TestShardMap_DeleteExpiredCollisionChain(t *testing.T) {
	shard := new(shardMap[string, string])

	now := fastime.UnixNanoNow()
	expiredTime := now - int64(100*time.Millisecond)
	futureTime := now + int64(1*time.Hour)

	// Build a hash collision bucket with 5 nodes on hash key 42:
	// n0: expired
	// n1: valid future
	// n2: expired
	// n3: tombstoned (state deleted)
	// n4: immortal (expire = 0 / NoTTL)
	const testHash = uint64(42)

	n4 := newGacheValue("k4_immortal", "v4", 0)
	n3 := newGacheValue("k3_deleted", "v3", expiredTime)
	n3.tombstone(shard.counter()) // the counter is reset to the live count below
	storeNext(n3, n4)
	n2 := newGacheValue("k2_expired", "v2", expiredTime)
	storeNext(n2, n3)
	n1 := newGacheValue("k1_future", "v1", futureTime)
	storeNext(n1, n2)
	n0 := newGacheValue("k0_expired", "v0", expiredTime)
	storeNext(n0, n1)

	// Install directly into dirty and counter with amended: true
	shard.mu.Lock()
	shard.dirty = map[uint64]*value[string, string]{testHash: n0}
	// Initial live count: 4 (n0, n1, n2, n4; n3 is deleted)
	shard.counter().Store(4)
	shard.read.Store(&readOnlyShard[string, string]{amended: true, c: shard.counter()})
	shard.mu.Unlock()

	var callbackKeys []string
	var callbackVals []string
	var cbMu sync.Mutex

	purged := shard.deleteExpired(now, func(k string, v string) {
		cbMu.Lock()
		defer cbMu.Unlock()
		callbackKeys = append(callbackKeys, k)
		callbackVals = append(callbackVals, v)
	})

	if purged != 2 {
		t.Fatalf("deleteExpired expected 2 purged nodes, got %d", purged)
	}

	// Shard counter should now be 4 - 2 = 2
	if rem := shard.length(); rem != 2 {
		t.Errorf("Expected remaining length 2, got %d", rem)
	}

	// Verify callbacks
	cbMu.Lock()
	defer cbMu.Unlock()
	if len(callbackKeys) != 2 {
		t.Fatalf("Expected 2 callbacks, got %d", len(callbackKeys))
	}
	expectedMap := map[string]string{
		"k0_expired": "v0",
		"k2_expired": "v2",
	}
	for i, k := range callbackKeys {
		v := callbackVals[i]
		expV, ok := expectedMap[k]
		if !ok || expV != v {
			t.Errorf("Unexpected callback for %s = %s", k, v)
		}
	}

	// Verify survivors can still be loaded
	if val, exp, ok := shard.loadValue(testHash, "k1_future"); !ok || val != "v1" || exp != futureTime {
		t.Errorf("k1_future not loadable: ok=%v, val=%v, exp=%v", ok, val, exp)
	}
	if val, _, ok := shard.loadValue(testHash, "k4_immortal"); !ok || val != "v4" {
		t.Errorf("k4_immortal not loadable: ok=%v, val=%v", ok, val)
	}

	// Verify expired nodes are now deleted
	if _, _, ok := shard.loadValue(testHash, "k0_expired"); ok {
		t.Errorf("k0_expired should not be loadable")
	}
	if _, _, ok := shard.loadValue(testHash, "k2_expired"); ok {
		t.Errorf("k2_expired should not be loadable")
	}

	// Calling deleteExpired second time should be a no-op
	purgedSecond := shard.deleteExpired(now, func(k string, v string) {
		t.Errorf("Callback should not fire on second deleteExpired: key=%s", k)
	})
	if purgedSecond != 0 {
		t.Errorf("Expected 0 purged on second run, got %d", purgedSecond)
	}
}

// Storing and deleting unique keys must not leave tombstones piling up in the
// dirty map: a delete unlinks a dirty-only node on the spot (popSlow). Every Map
// delete flavour must do it.
func TestShardMap_UniqueKeyChurnReleasesDirtyNodes(t *testing.T) {
	t.Parallel()

	const stable, churn = 10, 5000

	tests := []struct {
		del  func(m *Map[int, int], k int) bool
		name string
	}{
		{name: "Delete", del: func(m *Map[int, int], k int) bool { m.Delete(k); return true }},
		{name: "LoadAndDelete", del: func(m *Map[int, int], k int) bool { _, ok := m.LoadAndDelete(k); return ok }},
		{name: "LoadAndDeletePointer", del: func(m *Map[int, int], k int) bool { _, ok := m.LoadAndDeletePointer(k); return ok }},
		{name: "CompareAndDelete", del: func(m *Map[int, int], k int) bool { return m.CompareAndDelete(k, k) }},
		{name: "CompareAndSwapPointer to nil", del: func(m *Map[int, int], k int) bool {
			p, _ := m.LoadPointer(k)
			return m.CompareAndSwapPointer(k, p, nil)
		}},
		{name: "SwapPointer nil", del: func(m *Map[int, int], k int) bool { _, ok := m.SwapPointer(k, nil); return ok }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m Map[int, int]
			for i := range stable {
				m.Store(i, i)
			}
			m.Range(func(int, int) bool { return true }) // promote: the stable keys live in the read snapshot
			for i := stable; i < stable+churn; i++ {
				m.Store(i, i)
				if !tt.del(&m, i) {
					t.Fatalf("delete of key %d reported not found", i)
				}
			}
			if got := m.Len(); got != stable {
				t.Fatalf("Len = %d, want %d", got, stable)
			}
			m.m.mu.Lock()
			buckets := len(m.m.dirty)
			m.m.mu.Unlock()
			if buckets > stable {
				t.Fatalf("dirty map holds %d buckets after %d store/delete cycles, want <= %d", buckets, churn, stable)
			}
		})
	}
}

// Every Map value is stored as *V, a word-sized pointer. The node is marked with
// stPtr and its word is written with a barriered atomic.StorePointer (no uint64
// store, no mirror field), so val itself is the typed, GC-visible reference. This
// pins the node-level contract: the flag is set, and val tracks every
// store/swap/CAS, is nil after delete and is restored by the in-place revival.
func TestShardMap_PointerValueUsesStPtrBarrieredStore(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	h := maphash.Comparable(hashSeed, "k")
	p1, p2 := new(int), new(int)

	m.StorePointer("k", p1)
	n, ok := m.m.loadNode(h, "k")
	if !ok || n.val != p1 {
		t.Fatalf("after first store: node val = %p (found=%v), want %p", n.val, ok, p1)
	}
	if n.state.Load()&stPtr == 0 {
		t.Fatal("node of a Map value (*V) lacks the stPtr flag, its word would be stored without a write barrier")
	}
	m.StorePointer("k", p2)
	if n.val != p2 {
		t.Fatalf("after overwrite: node val = %p, want %p", n.val, p2)
	}
	if prev, loaded := m.SwapPointer("k", p1); !loaded || prev != p2 || n.val != p1 {
		t.Fatalf("after swap: prev = %p loaded = %v val = %p, want %p true %p", prev, loaded, n.val, p2, p1)
	}
	if !m.CompareAndSwapPointer("k", p1, p2) || n.val != p2 {
		t.Fatalf("after CAS: val = %p, want %p", n.val, p2)
	}
	m.Delete("k")
	if n.val != nil {
		t.Fatalf("after delete: node val = %p, want nil", n.val)
	}
	m.StorePointer("k", p1) // revives the tombstoned node in place
	if n.val != p1 {
		t.Fatalf("after revival: node val = %p, want %p", n.val, p1)
	}
}

// The same race on the shardMap that backs Map[K,V]: a stored *V is never nil,
// so a hit carrying a nil (or different) pointer is a revive that published the
// state before the payload. The public-API variant is TestMap_ReviveNeverExposesNilPointer.
func TestShardMap_ReviveNeverExposesNilPointer(t *testing.T) {
	t.Parallel()

	const h = 0x1234
	p := new(int)
	*p = 5

	t.Run("loadValue", func(t *testing.T) {
		t.Parallel()
		var sm shardMap[string, *int]
		hits, bad := runReviveChurn(
			func() { sm.store(h, "k", p, 0) },
			func() { sm.loadAndDelete(h, "k") },
			func() (bool, bool) { v, _, ok := sm.loadValue(h, "k"); return ok, v == p },
		)
		if bad != 0 {
			t.Fatalf("%d of %d loadValue hits returned a pointer other than the stored one", bad, hits)
		}
		if hits == 0 {
			t.Fatal("readers never observed the key; the race was not exercised")
		}
	})
	t.Run("loadOrStore", func(t *testing.T) {
		t.Parallel()
		var sm shardMap[string, *int]
		q := new(int)
		hits, bad := runReviveChurn(
			func() { sm.store(h, "k", p, 0) },
			func() { sm.loadAndDelete(h, "k") },
			func() (bool, bool) {
				// A reader that wins the race installs q, so q is as legitimate as p.
				v, loaded := sm.loadOrStore(h, "k", q, 0)
				return loaded, v == p || v == q
			},
		)
		if bad != 0 {
			t.Fatalf("%d of %d loadOrStore hits returned neither stored pointer", bad, hits)
		}
	})
}

// Promoting dirty to the read snapshot resets the miss counter: a stale count would
// promote the next dirty map after a single miss.
func TestShardMap_PromotionResetsMisses(t *testing.T) {
	t.Parallel()

	var sm shardMap[int, int]
	sm.store(1, 1, 10, 0)
	sm.store(2, 2, 20, 0)
	// Both keys live only in dirty: each load is a miss, the second reaches len(dirty).
	for h := uint64(1); h <= 2; h++ {
		if v, _, ok := sm.loadValue(h, int(h)); !ok || v != int(h)*10 {
			t.Fatalf("loadValue(%d) = (%d, %v)", h, v, ok)
		}
	}
	sm.mu.Lock()
	dirty, misses := sm.dirty, sm.misses
	sm.mu.Unlock()
	if dirty != nil || misses != 0 {
		t.Errorf("after the promotion dirty = %v, misses = %d, want nil and 0", dirty, misses)
	}
}
