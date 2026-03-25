package gache

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGetShardID_MaxKeyLengthZero tests getShardID when maxKeyLength (kl) is 0,
// meaning the full key is used for hashing.
func TestGetShardID_MaxKeyLengthZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
	}{
		{name: "single char key", key: "a"},
		{name: "two char key", key: "ab"},
		{name: "32 char key", key: strings.Repeat("x", 32)},
		{name: "33 char key (uses xxh3)", key: strings.Repeat("y", 33)},
		{name: "long key (uses xxh3)", key: strings.Repeat("z", 256)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id := getShardID(tt.key, 0)
			if id > mask {
				t.Errorf("getShardID(%q, 0) = %d, want <= %d (mask)", tt.key, id, mask)
			}
			// Result must be deterministic within the same process.
			id2 := getShardID(tt.key, 0)
			if id != id2 {
				t.Errorf("getShardID(%q, 0) is not deterministic: got %d then %d", tt.key, id, id2)
			}
		})
	}
}

// TestGetShardID_MaxKeyLengthOne tests getShardID when kl == 1,
// which should use only the first byte of the key.
func TestGetShardID_MaxKeyLengthOne(t *testing.T) {
	t.Parallel()

	// When kl=1, the shard ID is determined solely by the first byte.
	idA1 := getShardID("abc", 1)
	idA2 := getShardID("axyz", 1)
	if idA1 != idA2 {
		t.Errorf("keys with the same first byte should map to the same shard with kl=1: got %d and %d", idA1, idA2)
	}

	// Keys starting with different bytes should (in general) differ; verify bounds only.
	idB := getShardID("bcd", 1)
	if idB > mask {
		t.Errorf("getShardID(%q, 1) = %d, want <= %d (mask)", "bcd", idB, mask)
	}
	// Manually verify: first byte of "abc" is 'a' == 97; 97 & mask == 97.
	want := uint64('a') & mask
	if idA1 != want {
		t.Errorf("getShardID(%q, 1) = %d, want %d", "abc", idA1, want)
	}
}

// TestGetShardID_MaxKeyLengthBetween1And32 tests getShardID when kl is 2..32.
func TestGetShardID_MaxKeyLengthBetween1And32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kl   uint64
	}{
		{name: "kl=2", kl: 2},
		{name: "kl=16", kl: 16},
		{name: "kl=32", kl: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			longKey := strings.Repeat("a", 64)
			shortKey := longKey[:min(int(tt.kl/2+1), len(longKey))] // shorter than kl

			idLong := getShardID(longKey, tt.kl)
			if idLong > mask {
				t.Errorf("getShardID(longKey, %d) = %d, want <= %d (mask)", tt.kl, idLong, mask)
			}

			idShort := getShardID(shortKey, tt.kl)
			if idShort > mask {
				t.Errorf("getShardID(shortKey, %d) = %d, want <= %d (mask)", tt.kl, idShort, mask)
			}

			// Long keys truncated to kl bytes: two keys with the same first kl bytes
			// must hash to the same shard.
			longKey2 := strings.Repeat("a", 64) + "different-suffix"
			idLong2 := getShardID(longKey2, tt.kl)
			if idLong != idLong2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, idLong, idLong2)
			}

			// Determinism
			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGetShardID_MaxKeyLengthOver32 tests getShardID when kl > 32 (uses xxh3 path).
func TestGetShardID_MaxKeyLengthOver32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kl   uint64
	}{
		{name: "kl=33", kl: 33},
		{name: "kl=64", kl: 64},
		{name: "kl=256", kl: 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			longKey := strings.Repeat("b", int(tt.kl)*2)
			shortKey := strings.Repeat("b", int(tt.kl)/2)

			idLong := getShardID(longKey, tt.kl)
			if idLong > mask {
				t.Errorf("getShardID(longKey, %d) = %d, want <= %d (mask)", tt.kl, idLong, mask)
			}

			idShort := getShardID(shortKey, tt.kl)
			if idShort > mask {
				t.Errorf("getShardID(shortKey, %d) = %d, want <= %d (mask)", tt.kl, idShort, mask)
			}

			// Two keys with the same prefix of length kl must hash to the same shard.
			key1 := strings.Repeat("c", int(tt.kl)) + "suffix1"
			key2 := strings.Repeat("c", int(tt.kl)) + "suffix2"
			id1 := getShardID(key1, tt.kl)
			id2 := getShardID(key2, tt.kl)
			if id1 != id2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, id1, id2)
			}

			// Determinism
			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGetShardID_KeyShorterThanMaxKeyLength tests that when the key is shorter
// than kl, the full key is used.
func TestGetShardID_KeyShorterThanMaxKeyLength(t *testing.T) {
	t.Parallel()

	// "hi" has length 2; kl=100 → effective kl = min(2,100) = 2.
	// Should equal getShardID("hi", 2).
	key := "hi"
	id1 := getShardID(key, 100)
	id2 := getShardID(key, 2)
	if id1 != id2 {
		t.Errorf("short key: getShardID(%q, 100)=%d should equal getShardID(%q, 2)=%d", key, id1, key, id2)
	}

	// Single-byte key with large kl: effective kl=1, must equal first-byte path.
	singleKey := "x"
	idSingle := getShardID(singleKey, 50)
	want := uint64(singleKey[0]) & mask
	if idSingle != want {
		t.Errorf("getShardID(%q, 50) = %d, want %d", singleKey, idSingle, want)
	}
}

// TestGetShardID_ResultInRange verifies the result is always within [0, mask].
func TestGetShardID_ResultInRange(t *testing.T) {
	t.Parallel()

	type tc struct {
		key string
		kl  uint64
	}
	cases := []tc{
		{"a", 0},
		{"a", 1},
		{"hello", 0},
		{"hello", 3},
		{strings.Repeat("k", 32), 0},
		{strings.Repeat("k", 32), 32},
		{strings.Repeat("k", 33), 0},
		{strings.Repeat("k", 33), 33},
		{strings.Repeat("k", 256), 64},
		{strings.Repeat("k", 256), 256},
	}

	for _, c := range cases {
		id := getShardID(c.key, c.kl)
		if id > mask {
			t.Errorf("getShardID(%q, %d) = %d exceeds mask %d", c.key, c.kl, id, mask)
		}
	}
}

// TestGetShardID_PrefixIsolation verifies that two keys differing only in bytes
// beyond position kl are assigned the same shard.
func TestGetShardID_PrefixIsolation(t *testing.T) {
	t.Parallel()

	prefix := strings.Repeat("p", 16)
	key1 := prefix + "AAAAAA"
	key2 := prefix + "BBBBBB"

	// With kl=16, only the first 16 bytes matter.
	id1 := getShardID(key1, 16)
	id2 := getShardID(key2, 16)
	if id1 != id2 {
		t.Errorf("prefix isolation failed: getShardID(%q, 16)=%d != getShardID(%q, 16)=%d", key1, id1, key2, id2)
	}

	// With kl > 32 (xxh3 path), same check.
	longPrefix := strings.Repeat("q", 50)
	key3 := longPrefix + "SUFFIX1"
	key4 := longPrefix + "SUFFIX2"
	id3 := getShardID(key3, 50)
	id4 := getShardID(key4, 50)
	if id3 != id4 {
		t.Errorf("prefix isolation (xxh3) failed: getShardID(%q, 50)=%d != getShardID(%q, 50)=%d", key3, id3, key4, id4)
	}
}

func TestGacheLenBasic(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL))

	if got := g.Len(); got != 0 {
		t.Fatalf("empty gache Len() = %d, want 0", got)
	}

	// Set increments
	g.Set("a", 1)
	g.Set("b", 2)
	g.Set("c", 3)
	if got := g.Len(); got != 3 {
		t.Fatalf("after 3 Sets, Len() = %d, want 3", got)
	}

	// Overwrite does not change count
	g.Set("b", 20)
	if got := g.Len(); got != 3 {
		t.Fatalf("after overwrite, Len() = %d, want 3", got)
	}

	// Delete decrements
	g.Delete("a")
	if got := g.Len(); got != 2 {
		t.Fatalf("after Delete, Len() = %d, want 2", got)
	}

	// Delete non-existent key is a no-op
	g.Delete("nonexistent")
	if got := g.Len(); got != 2 {
		t.Fatalf("after Delete(nonexistent), Len() = %d, want 2", got)
	}

	// Pop decrements
	if _, ok := g.Pop("b"); !ok {
		t.Fatal("Pop(b) returned ok=false")
	}
	if got := g.Len(); got != 1 {
		t.Fatalf("after Pop, Len() = %d, want 1", got)
	}

	// SetIfNotExists with new key increments
	g.SetIfNotExists("d", 4)
	if got := g.Len(); got != 2 {
		t.Fatalf("after SetIfNotExists(new), Len() = %d, want 2", got)
	}

	// SetIfNotExists with existing key does not change count
	g.SetIfNotExists("d", 40)
	if got := g.Len(); got != 2 {
		t.Fatalf("after SetIfNotExists(existing), Len() = %d, want 2", got)
	}

	// Clear resets to 0
	g.Clear()
	if got := g.Len(); got != 0 {
		t.Fatalf("after Clear, Len() = %d, want 0", got)
	}
}

func TestGacheLenConcurrent(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL)).(*gache[int])

	const (
		numGoroutines   = 16
		opsPerGoroutine = 1000
		keyRange        = 200
	)

	var wg sync.WaitGroup
	for id := range numGoroutines {
		wg.Go(func() {
			r := rand.New(rand.NewSource(int64(id)))
			for i := range opsPerGoroutine {
				key := fmt.Sprintf("key-%d", r.Intn(keyRange))
				switch r.Intn(2) {
				case 0:
					g.Set(key, i)
				case 1:
					g.Delete(key)
				}
			}
		})
	}
	wg.Wait()

	// Verify Len matches actual count from Range
	actual := 0
	for i := range g.shards {
		g.shards[i].Range(func(k string, v value[int]) bool {
			actual++
			return true
		})
	}

	if got := g.Len(); got != actual {
		t.Fatalf("after concurrent ops, Len() = %d, but counted %d entries", got, actual)
	}
}

func TestGacheLenConcurrentStoreDelete(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL)).(*gache[int])

	const (
		numGoroutines    = 8
		keysPerGoroutine = 500
	)

	// Phase 1: Store unique keys concurrently
	var wg sync.WaitGroup
	for id := range numGoroutines {
		wg.Go(func() {
			base := id * keysPerGoroutine
			for i := range keysPerGoroutine {
				g.Set(fmt.Sprintf("key-%d", base+i), i)
			}
		})
	}
	wg.Wait()

	total := numGoroutines * keysPerGoroutine
	if got := g.Len(); got != total {
		t.Fatalf("after storing %d unique keys, Len() = %d", total, got)
	}

	// Phase 2: Delete all keys concurrently
	for id := range numGoroutines {
		wg.Go(func() {
			base := id * keysPerGoroutine
			for i := range keysPerGoroutine {
				g.Delete(fmt.Sprintf("key-%d", base+i))
			}
		})
	}
	wg.Wait()

	if got := g.Len(); got != 0 {
		t.Fatalf("after deleting all keys, Len() = %d, want 0", got)
	}
}

func TestGacheLenClearConcurrent(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL)).(*gache[int])

	const (
		numWriters  = 4
		numDeleters = 4
		clearCycles = 200
	)

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Writers
	for id := range numWriters {
		wg.Go(func() {
			r := rand.New(rand.NewSource(int64(id)))
			for {
				select {
				case <-done:
					return
				default:
					g.Set(fmt.Sprintf("k-%d", r.Intn(100)), id)
				}
			}
		})
	}

	// Deleters
	for id := range numDeleters {
		wg.Go(func() {
			r := rand.New(rand.NewSource(int64(id + 100)))
			for {
				select {
				case <-done:
					return
				default:
					g.Delete(fmt.Sprintf("k-%d", r.Intn(100)))
				}
			}
		})
	}

	// Periodically Clear
	for range clearCycles {
		g.Clear()
		time.Sleep(time.Microsecond)
	}

	close(done)
	wg.Wait()

	// Final check: Len matches actual count after all goroutines have stopped
	actual := 0
	for i := range g.shards {
		g.shards[i].Range(func(k string, v value[int]) bool {
			actual++
			return true
		})
	}
	if got := g.Len(); got != actual {
		t.Fatalf("final Len() = %d, counted %d entries", got, actual)
	}
}

func TestDataRace(t *testing.T) {
	c := New[string]()
	c.Set("key", "value")

	var wg sync.WaitGroup
	const (
		numGoroutines = 100
		iterations    = 1000
	)

	// Vary access patterns, increase goroutines, introduce delays
	for range numGoroutines {
		wg.Go(func() {
			for j := range iterations {
				// Introduce random delay
				time.Sleep(time.Duration(rand.Intn(5)) * time.Nanosecond)

				key := fmt.Sprintf("key_%d", j%50)
				val := fmt.Sprintf("val_%d", j)

				switch rand.Intn(13) {
				case 0:
					c.Get(key)
				case 1:
					c.Set(key, val)
				case 2:
					c.Delete(key)
				case 3:
					c.SetWithExpire(key, val, time.Millisecond*10)
				case 4:
					c.GetWithExpire(key)
				case 5:
					c.Pop(key)
				case 6:
					c.SetIfNotExists(key, val)
				case 7:
					c.ExtendExpire(key, time.Millisecond*5)
				case 8:
					c.GetRefresh(key)
				case 9:
					c.Len()
				case 10:
					c.Keys(t.Context())
				case 11:
					c.Values(t.Context())
				case 12:
					c.Range(t.Context(), func(k string, v string, exp int64) bool {
						return true
					})
				}
			}
		})
	}

	wg.Wait()

	// Assert final state consistency
	c.Clear()
	if l := c.Len(); l != 0 {
		t.Fatalf("expected length 0 after Clear, got %d", l)
	}

	c.Set("final_key", "final_value")
	if v, ok := c.Get("final_key"); !ok || v != "final_value" {
		t.Fatalf("expected 'final_value', got %v (ok: %t)", v, ok)
	}

	if l := c.Len(); l != 1 {
		t.Fatalf("expected length 1 after setting final key, got %d", l)
	}

	keys := c.Keys(t.Context())
	if len(keys) != 1 || keys[0] != "final_key" {
		t.Fatalf("expected only 'final_key' in keys, got %v", keys)
	}
}
