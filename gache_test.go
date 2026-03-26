package gache

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func forceBug(t *testing.T) {
	t.Helper()
	gc := New[int]()
	gc.Set("old_key", 42)
	shard := gc.(*gache[int]).shards[getShardID("old_key", gc.(*gache[int]).maxKeyLength)]
	shard.Load("nonexistent1")
	shard.Load("nonexistent2")
	shard.Load("nonexistent3")

	shard.InitReserve(10)

	for i := range 10000 {
		key := fmt.Sprintf("k_%d", i)
		if getShardID(key, gc.(*gache[int]).maxKeyLength) == getShardID("old_key", gc.(*gache[int]).maxKeyLength) {
			gc.Set(key, 99)
			break
		}
	}

	for i := range 100 {
		shard.Load(fmt.Sprintf("miss_%d", i))
	}

	if val, ok := gc.Get("old_key"); !ok || val != 42 {
		t.Fatalf("Lost old key! ok=%v, val=%v", ok, val)
	}
}

// TestGache_DisableExpiredHook ensures that disabling an active expiration hook correctly prevents subsequent callbacks from executing.
func TestGache_DisableExpiredHook(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	expiredChan := make(chan string, 1)

	gc := New[string]().
		SetExpiredHook(func(ctx context.Context, key string, val string) {
			expiredChan <- val
		}).
		EnableExpiredHook().
		DisableExpiredHook().
		StartExpired(ctx, 10*time.Millisecond)

	gc.SetWithExpire("hook_key", "hook_value", 50*time.Millisecond)

	select {
	case <-expiredChan:
		t.Error("expected expired hook to be disabled")
	case <-time.After(200 * time.Millisecond):
		// success, it was disabled

	}
}

// TestGache_ExpiredHook verifies that registered callbacks are properly triggered when an item exceeds its TTL limit.
func TestGache_ExpiredHook(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	expiredChan := make(chan string, 1)

	gc := New[string]().
		SetExpiredHook(func(ctx context.Context, key string, val string) {
			expiredChan <- val
		}).
		EnableExpiredHook().
		StartExpired(ctx, 10*time.Millisecond)

	gc.SetWithExpire("hook_key", "hook_value", 50*time.Millisecond)

	select {
	case val := <-expiredChan:
		if val != "hook_value" {
			t.Errorf("expected 'hook_value', got %v", val)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for expired hook")
	}
}

// TestGache_ExtendExpire verifies that manually extending a key's expiration successfully prolongs its lifetime in the cache.
func TestGache_ExtendExpire(t *testing.T) {
	t.Helper()
	gc := New[string]()
	gc.SetWithExpire("key", "value", 100*time.Millisecond)
	gc.ExtendExpire("key", 500*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	if _, ok := gc.Get("key"); !ok {
		t.Error("expected key to still exist after ExtendExpire")
	}
}

// TestGache_GetRefresh tests whether fetching an item using GetRefresh correctly retrieves the value and extends its expiration window.
func TestGache_GetRefresh(t *testing.T) {
	t.Helper()
	gc := New[string]().SetDefaultExpire(500 * time.Millisecond)
	gc.SetWithExpire("key", "value", 100*time.Millisecond)

	v, ok := gc.GetRefresh("key")
	if !ok || v != "value" {
		t.Errorf("expected to get 'value', got %v", v)
	}

	time.Sleep(200 * time.Millisecond)

	if _, ok := gc.Get("key"); !ok {
		t.Error("expected key to still exist after GetRefresh")
	}
}

// TestGache_Pop guarantees that invoking Pop accurately returns the value and simultaneously removes the key from the cache.
func TestGache_Pop(t *testing.T) {
	t.Helper()
	gc := New[string]()
	gc.Set("key", "value")
	v, ok := gc.Pop("key")
	if !ok || v != "value" {
		t.Errorf("expected to pop 'value', got %v", v)
	}
	if _, ok := gc.Get("key"); ok {
		t.Error("expected key to be removed after Pop")
	}
}

// TestGache_SetIfNotExists verifies that setting a value conditionally only succeeds when the underlying key is completely absent.
func TestGache_SetIfNotExists(t *testing.T) {
	t.Helper()
	gc := New[string]()
	gc.SetIfNotExists("key", "value1")
	if v, ok := gc.Get("key"); !ok || v != "value1" {
		t.Errorf("expected value1, got %v", v)
	}
	gc.SetIfNotExists("key", "value2")
	if v, ok := gc.Get("key"); !ok || v != "value1" {
		t.Errorf("expected value1 after second SetIfNotExists, got %v", v)
	}
}

// TestGache_DataRace rigorously hammers the cache with concurrent mixed operations to expose any potential data races or synchronization flaws.
func TestGache_DataRace(t *testing.T) {
	c := New[string]()
	c.Set("key", "value")

	var wg sync.WaitGroup
	const (
		numGoroutines = 100
		iterations    = 1000
	)

	for range numGoroutines {
		wg.Go(func() {
			for j := range iterations {

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

// TestGache_ForceBug is a specialized test designed to trigger edge-case hash collisions and verify the cache's resilience to map state corruption.
func TestGache_ForceBug(t *testing.T) {
	forceBug(t)
}

// TestGache_GetShardID_KeyShorterThanMaxKeyLength verifies that keys shorter than the configured maximum length are hashed entirely without truncation.
func TestGache_GetShardID_KeyShorterThanMaxKeyLength(t *testing.T) {
	t.Parallel()

	key := "hi"
	id1 := getShardID(key, 100)
	id2 := getShardID(key, 2)
	if id1 != id2 {
		t.Errorf("short key: getShardID(%q, 100)=%d should equal getShardID(%q, 2)=%d", key, id1, key, id2)
	}

	singleKey := "x"
	idSingle := getShardID(singleKey, 50)
	want := uint64(singleKey[0]) & mask
	if idSingle != want {
		t.Errorf("getShardID(%q, 50) = %d, want %d", singleKey, idSingle, want)
	}
}

// TestGache_GetShardID_MaxKeyLengthBetween1And32 validates the hashing logic when a key's length boundary falls within the 1-32 byte range.
func TestGache_GetShardID_MaxKeyLengthBetween1And32(t *testing.T) {
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
			shortKey := longKey[:min(int(tt.kl/2+1), len(longKey))]

			idLong := getShardID(longKey, tt.kl)
			if idLong > mask {
				t.Errorf("getShardID(longKey, %d) = %d, want <= %d (mask)", tt.kl, idLong, mask)
			}

			idShort := getShardID(shortKey, tt.kl)
			if idShort > mask {
				t.Errorf("getShardID(shortKey, %d) = %d, want <= %d (mask)", tt.kl, idShort, mask)
			}

			longKey2 := strings.Repeat("a", 64) + "different-suffix"
			idLong2 := getShardID(longKey2, tt.kl)
			if idLong != idLong2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, idLong, idLong2)
			}

			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGache_GetShardID_MaxKeyLengthOne ensures that configuring a maximum key length of 1 correctly distributes keys based exclusively on their first byte.
func TestGache_GetShardID_MaxKeyLengthOne(t *testing.T) {
	t.Parallel()

	idA1 := getShardID("abc", 1)
	idA2 := getShardID("axyz", 1)
	if idA1 != idA2 {
		t.Errorf("keys with the same first byte should map to the same shard with kl=1: got %d and %d", idA1, idA2)
	}

	idB := getShardID("bcd", 1)
	if idB > mask {
		t.Errorf("getShardID(%q, 1) = %d, want <= %d (mask)", "bcd", idB, mask)
	}

	want := uint64('a') & mask
	if idA1 != want {
		t.Errorf("getShardID(%q, 1) = %d, want %d", "abc", idA1, want)
	}
}

// TestGache_GetShardID_MaxKeyLengthOver32 tests the xxh3 fallback hashing algorithm utilized for key lengths exceeding 32 bytes.
func TestGache_GetShardID_MaxKeyLengthOver32(t *testing.T) {
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

			key1 := strings.Repeat("c", int(tt.kl)) + "suffix1"
			key2 := strings.Repeat("c", int(tt.kl)) + "suffix2"
			id1 := getShardID(key1, tt.kl)
			id2 := getShardID(key2, tt.kl)
			if id1 != id2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, id1, id2)
			}

			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGache_GetShardID_MaxKeyLengthZero confirms that providing a zero max length forces the hasher to digest the entire string unconditionally.
func TestGache_GetShardID_MaxKeyLengthZero(t *testing.T) {
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

			id2 := getShardID(tt.key, 0)
			if id != id2 {
				t.Errorf("getShardID(%q, 0) is not deterministic: got %d then %d", tt.key, id, id2)
			}
		})
	}
}

// TestGache_GetShardID_PrefixIsolation verifies that keys sharing identical prefixes up to the max length correctly hash to the exact same shard.
func TestGache_GetShardID_PrefixIsolation(t *testing.T) {
	t.Parallel()

	prefix := strings.Repeat("p", 16)
	key1 := prefix + "AAAAAA"
	key2 := prefix + "BBBBBB"

	id1 := getShardID(key1, 16)
	id2 := getShardID(key2, 16)
	if id1 != id2 {
		t.Errorf("prefix isolation failed: getShardID(%q, 16)=%d != getShardID(%q, 16)=%d", key1, id1, key2, id2)
	}

	longPrefix := strings.Repeat("q", 50)
	key3 := longPrefix + "SUFFIX1"
	key4 := longPrefix + "SUFFIX2"
	id3 := getShardID(key3, 50)
	id4 := getShardID(key4, 50)
	if id3 != id4 {
		t.Errorf("prefix isolation (xxh3) failed: getShardID(%q, 50)=%d != getShardID(%q, 50)=%d", key3, id3, key4, id4)
	}
}

// TestGache_GetShardID_ResultInRange ensures the calculated shard ID always falls safely within the bounds of the allocated shard array.
func TestGache_GetShardID_ResultInRange(t *testing.T) {
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

// TestGache_LenBasic validates the fundamental item counting mechanism under standard single-threaded sequential insertions and deletions.
func TestGache_LenBasic(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL))

	if got := g.Len(); got != 0 {
		t.Fatalf("empty gache Len() = %d, want 0", got)
	}

	g.Set("a", 1)
	g.Set("b", 2)
	g.Set("c", 3)
	if got := g.Len(); got != 3 {
		t.Fatalf("after 3 Sets, Len() = %d, want 3", got)
	}

	g.Set("b", 20)
	if got := g.Len(); got != 3 {
		t.Fatalf("after overwrite, Len() = %d, want 3", got)
	}

	g.Delete("a")
	if got := g.Len(); got != 2 {
		t.Fatalf("after Delete, Len() = %d, want 2", got)
	}

	g.Delete("nonexistent")
	if got := g.Len(); got != 2 {
		t.Fatalf("after Delete(nonexistent), Len() = %d, want 2", got)
	}

	if _, ok := g.Pop("b"); !ok {
		t.Fatal("Pop(b) returned ok=false")
	}
	if got := g.Len(); got != 1 {
		t.Fatalf("after Pop, Len() = %d, want 1", got)
	}

	g.SetIfNotExists("d", 4)
	if got := g.Len(); got != 2 {
		t.Fatalf("after SetIfNotExists(new), Len() = %d, want 2", got)
	}

	g.SetIfNotExists("d", 40)
	if got := g.Len(); got != 2 {
		t.Fatalf("after SetIfNotExists(existing), Len() = %d, want 2", got)
	}

	g.Clear()
	if got := g.Len(); got != 0 {
		t.Fatalf("after Clear, Len() = %d, want 0", got)
	}
}

// TestGache_LenClearConcurrent ensures that clearing the cache during heavy concurrent activity results in a correct and consistent item count.
func TestGache_LenClearConcurrent(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL)).(*gache[int])

	const (
		numWriters  = 4
		numDeleters = 4
		clearCycles = 200
	)

	done := make(chan struct{})
	var wg sync.WaitGroup

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

	for range clearCycles {
		g.Clear()
		time.Sleep(time.Microsecond)
	}

	close(done)
	wg.Wait()

	actual := 0
	for i := range g.shards {
		g.shards[i].RangePointer(func(k string, v *value[int]) bool {
			actual++
			return true
		})
	}
	if got := g.Len(); got != actual {
		t.Fatalf("final Len() = %d, counted %d entries", got, actual)
	}
}

// TestGache_LenConcurrent verifies that the cache's length accurately reflects the number of items stored during intense parallel modifications.
func TestGache_LenConcurrent(t *testing.T) {
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

	actual := 0
	for i := range g.shards {
		g.shards[i].RangePointer(func(k string, v *value[int]) bool {
			actual++
			return true
		})
	}

	if got := g.Len(); got != actual {
		t.Fatalf("after concurrent ops, Len() = %d, but counted %d entries", got, actual)
	}
}

// TestGache_LenConcurrentStoreDelete tests the integrity of the item counter when multiple goroutines symmetrically store and delete items.
func TestGache_LenConcurrentStoreDelete(t *testing.T) {
	t.Parallel()
	g := New[int](WithDefaultExpiration[int](NoTTL)).(*gache[int])

	const (
		numGoroutines    = 8
		keysPerGoroutine = 500
	)

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

// TestGache_ReadDoesNotDropExisting confirms that reading state into the cache from a serialized stream does not erroneously overwrite or discard existing uncollided keys.
func TestGache_ReadDoesNotDropExisting(t *testing.T) {
	gc := New[int]()
	gc.Set("old_key", 42)
	gc.Get("old_key1")
	shard := gc.(*gache[int]).shards[getShardID("old_key", gc.(*gache[int]).maxKeyLength)]
	shard.Load("nonexistent1")
	shard.Load("nonexistent2")
	shard.Load("nonexistent3")

	var buf bytes.Buffer
	m := map[string]int{"new_key": 99}
	for i := range 4096 * 2 {
		m[fmt.Sprintf("k%d", i)] = i
	}
	gob.Register(map[string]int{})
	err := gob.NewEncoder(&buf).Encode(&m)
	if err != nil {
		t.Fatal(err)
	}

	err = gc.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}

	for i := range 10000 {
		shard.Load(fmt.Sprintf("miss_%d", i))
	}

	if val, ok := gc.Get("old_key"); !ok || val != 42 {
		t.Fatalf("Lost old key! ok=%v, val=%v", ok, val)
	}
}

// TestGache_ConcurrentOperations systematically exercises all primary API endpoints across multiple goroutines to validate thread safety.
func TestGache_ConcurrentOperations(t *testing.T) {
	g := New[string]().SetDefaultExpire(1 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	const numRoutines = 50
	const numKeys = 100

	for range numRoutines {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					key := fmt.Sprintf("key-%d", rand.Intn(numKeys))
					op := rand.Intn(10)
					switch op {
					case 0, 1, 2:
						g.Set(key, "value")
					case 3, 4, 5:
						g.Get(key)
					case 6:
						g.Delete(key)
					case 7:
						g.SetWithExpire(key, "value", 10*time.Millisecond)
					case 8:
						g.Keys(context.Background())
					case 9:
						g.Values(context.Background())
					}
				}
			}
		})
	}
	wg.Wait()
}

// TestGache_ContextCancellation verifies that background tasks and prolonged range iterations correctly halt upon context cancellation.
func TestGache_ContextCancellation(t *testing.T) {
	g := New[int]()
	for i := range 10000 {
		g.Set(fmt.Sprintf("k-%d", i), i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int32
	g.Range(ctx, func(k string, v int, exp int64) bool {
		c := count.Add(1)
		if c == 100 {
			cancel()
		}
		time.Sleep(10 * time.Microsecond)
		return true
	})

	if count.Load() >= 10000 {
		t.Fatalf("expected early termination, got %d", count.Load())
	}
}
