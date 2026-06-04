package gache

import (
	"fmt"
	"sync"
	"testing"
)

// buildCollisionShard returns a shardMap[int] pre-seeded with `n` entries
// that all share the same uint64 hash h, exercising the collision chain.
// We bypass the gache layer and test shardMap directly.
func buildCollisionShard(h uint64, keys []string) *shardMap[int] {
	m := &shardMap[int]{}
	for i, k := range keys {
		m.store(h, k, i+1, -1)
	}
	return m
}

func TestShardMapCollision_StoreLoad(t *testing.T) {
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

func TestShardMapCollision_DeleteAndStore(t *testing.T) {
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

func TestShardMapCollision_Concurrent(t *testing.T) {
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

func TestShardMapCollision_LenConsistency(t *testing.T) {
	t.Parallel()
	h := uint64(0xdeadbeef)
	const nkeys = 10

	keys := make([]string, nkeys)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}

	m := buildCollisionShard(h, keys)
	if l := m.length_(); l != nkeys {
		t.Fatalf("after insert: length=%d, want %d", l, nkeys)
	}

	// Delete half
	for i := range nkeys / 2 {
		m.loadAndDelete(h, fmt.Sprintf("key%d", i))
	}
	if l := m.length_(); l != nkeys/2 {
		t.Fatalf("after delete: length=%d, want %d", l, nkeys/2)
	}

	// Revive all (store on tombstoned keys)
	for i := range nkeys {
		m.store(h, fmt.Sprintf("key%d", i), i*10, -1)
	}
	if l := m.length_(); l != nkeys {
		t.Fatalf("after revive: length=%d, want %d", l, nkeys)
	}
}
