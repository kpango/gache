package gache_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gache "github.com/kpango/gache/v2"
)

func TestGache_Basic(t *testing.T) {
	g := gache.New[string]()

	g.Set("key1", "value1")
	if v, ok := g.Get("key1"); !ok || v != "value1" {
		t.Errorf("Get(key1) = %v, %v; want value1, true", v, ok)
	}

	g.Delete("key1")
	if _, ok := g.Get("key1"); ok {
		t.Errorf("Get(key1) after Delete = true; want false")
	}
}

func TestGache_Expiration(t *testing.T) {
	g := gache.New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enable hook to verify expiration event
	expiredKeys := make(chan string, 10)
	g.EnableExpiredHook().SetExpiredHook(func(ctx context.Context, key string, val int) {
		expiredKeys <- key
	})

	// Start expiration daemon (tick every 100ms)
	g.StartExpired(ctx, 100*time.Millisecond)

	// Set with 200ms expiration
	g.SetWithExpire("exp1", 100, 200*time.Millisecond)

	// Immediately it should be there
	if _, ok := g.Get("exp1"); !ok {
		t.Fatal("Get(exp1) should be found immediately")
	}

	// Wait for expiration (allow some buffer for ticker and processing)
	// Ticker runs every 100ms.
	// Time caching runs every 100ms.
	// 200ms expire.
	// Wait 1s to be safe.
	time.Sleep(1 * time.Second)

	if _, ok := g.Get("exp1"); ok {
		t.Error("Get(exp1) should be expired and removed")
	}

	// Verify hook
	select {
	case k := <-expiredKeys:
		if k != "exp1" {
			t.Errorf("Expired hook got key %s, want exp1", k)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Expired hook not triggered")
	}
}

func TestGache_Range(t *testing.T) {
	g := gache.New[int]()
	count := 100
	for i := 0; i < count; i++ {
		g.Set(fmt.Sprintf("k%d", i), i)
	}

	found := 0
	g.Range(context.Background(), func(k string, v int, exp int64) bool {
		found++
		return true
	})

	if found != count {
		t.Errorf("Range found %d items, want %d", found, count)
	}
}

func TestGache_Clear(t *testing.T) {
	g := gache.New[int]()
	g.Set("k1", 1)
	g.Clear()
	if g.Len() != 0 {
		t.Errorf("Len() after Clear = %d, want 0", g.Len())
	}
	if _, ok := g.Get("k1"); ok {
		t.Error("Get(k1) after Clear = true, want false")
	}
}

func TestGache_Concurrency(t *testing.T) {
	g := gache.New[int]()
	var wg sync.WaitGroup
	workers := 10
	ops := 1000

	// Concurrent Sets
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("k-%d-%d", id, j)
				g.Set(key, j)
			}
		}(i)
	}
	wg.Wait()

	if g.Len() != workers*ops {
		t.Errorf("Len() = %d, want %d", g.Len(), workers*ops)
	}

	// Concurrent Gets and Deletes
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := fmt.Sprintf("k-%d-%d", id, j)
				if _, ok := g.Get(key); !ok {
					t.Errorf("Get(%s) failed", key)
				}
				g.Delete(key)
			}
		}(i)
	}
	wg.Wait()

	if g.Len() != 0 {
		t.Errorf("Len() after delete all = %d, want 0", g.Len())
	}
}

func TestGache_UpdateTTL(t *testing.T) {
	g := gache.New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.StartExpired(ctx, 100*time.Millisecond)

	// Set with short TTL
	g.SetWithExpire("k1", 1, 500*time.Millisecond)

	// Update with longer TTL
	g.SetWithExpire("k1", 2, 2*time.Second)

	// Wait 1s (should not expire)
	time.Sleep(1 * time.Second)

	if v, ok := g.Get("k1"); !ok || v != 2 {
		t.Errorf("Get(k1) = %v, %v; want 2, true (should be extended)", v, ok)
	}
}
