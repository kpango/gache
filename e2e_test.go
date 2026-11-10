package gache_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gache "github.com/kpango/gache/v2"
)

// End-to-end scenarios: the expiry daemon and the expired hook running for real,
// concurrent operations that must stay consistent together, and production-like
// workloads. Per-method behavior lives in the contract matrices of gache_test.go
// and map_test.go.

// The daemon sweeps every expired entry, and the expired hook runs exactly once for
// each of them while it is enabled and not at all once it is disabled. Every wait
// follows progress (the entry count, the hook count) instead of a fixed deadline: on
// a loaded -race host the 5ms cache clock and the daemon tick lag by far more than
// any fixed deadline, which is not a stall.
func TestE2E_Lifecycle(t *testing.T) {
	t.Parallel()

	t.Run("daemon sweeps expired entries", func(t *testing.T) {
		t.Parallel()

		const n = 40
		gc := gache.New[string]()
		for i := range n {
			gc.SetWithExpire(fmt.Sprintf("exp_daemon_%d", i), "data", 30*time.Millisecond)
		}
		gc.StartExpired(t.Context(), 15*time.Millisecond)
		t.Cleanup(gc.Stop)

		gache.WaitUntilProgress(t, "the daemon to sweep every expired entry",
			func() bool { return gc.Len() == 0 }, gc.Len)
		gc.Stop() // twice (with Cleanup): neither panics
	})

	t.Run("expired hook runs once per entry and not while disabled", func(t *testing.T) {
		t.Parallel()

		var (
			mu    sync.Mutex
			calls = map[string]int{}
			total atomic.Int32
		)
		gc := gache.New[string]().
			SetExpiredHook(func(_ context.Context, key, _ string) {
				mu.Lock()
				calls[key]++
				mu.Unlock()
				total.Add(1)
			}).
			EnableExpiredHook().
			StartExpired(t.Context(), 15*time.Millisecond)
		t.Cleanup(gc.Stop)

		const n = 10
		for i := range n {
			gc.SetWithExpire(fmt.Sprintf("hook_test_%d", i), "val", 25*time.Millisecond)
		}
		gache.WaitUntilProgress(t, "the expired hook to run for every entry",
			func() bool { return total.Load() >= n }, func() int { return int(total.Load()) })
		gache.WaitUntilProgress(t, "the daemon to sweep the first batch", func() bool { return gc.Len() == 0 }, gc.Len)

		gc.DisableExpiredHook()
		for i := n; i < 2*n; i++ {
			gc.SetWithExpire(fmt.Sprintf("hook_test_%d", i), "val", 25*time.Millisecond)
		}
		gache.WaitUntilProgress(t, "the daemon to sweep the second batch", func() bool { return gc.Len() == 0 }, gc.Len)

		mu.Lock()
		defer mu.Unlock()
		for i := range 2 * n {
			k, want := fmt.Sprintf("hook_test_%d", i), 1
			if i >= n {
				want = 0 // swept while the hook was disabled
			}
			if calls[k] != want {
				t.Errorf("hook ran %d times for %s, want %d", calls[k], k, want)
			}
		}
	})
}

// ============================================================================
// Concurrent combinations
// ============================================================================

func TestE2E_Combinations(t *testing.T) {
	t.Parallel()

	t.Run("ConcurrentPopAndExpirationSweep", testE2E_Combination_ConcurrentPopAndExpirationSweep)
	t.Run("ConcurrentExtendExpireAndGetRefresh", testE2E_Combination_ConcurrentExtendExpireAndGetRefresh)
	t.Run("ConcurrentSetIfNotExistsContention", testE2E_Combination_ConcurrentSetIfNotExistsContention)
}

func testE2E_Combination_ConcurrentPopAndExpirationSweep(t *testing.T) {
	t.Parallel()
	gc := gache.New[int]()
	const n = 1000

	// Populate keys with fast expiration
	for i := range n {
		gc.SetWithExpire(fmt.Sprintf("pop_sweep_%d", i), i, 30*time.Millisecond)
	}

	var poppedCount atomic.Uint64
	var wg sync.WaitGroup

	// Concurrent Pop workers
	for i := range 10 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := workerID; j < n; j += 10 {
				if _, ok := gc.Pop(fmt.Sprintf("pop_sweep_%d", j)); ok {
					poppedCount.Add(1)
				}
			}
		}(i)
	}

	// Concurrent DeleteExpired caller
	wg.Add(1)
	var sweptCount atomic.Uint64
	go func() {
		defer wg.Done()
		for range 5 {
			time.Sleep(10 * time.Millisecond)
			swept := gc.DeleteExpired(t.Context())
			sweptCount.Add(swept)
		}
	}()

	wg.Wait()

	// No key is removed twice (popped + swept never exceeds n). Every key was
	// visited by a Pop, which removes it live (counted) or, if its TTL passed first,
	// evicts it unreturned, so none is left; the split between popped, swept and
	// evicted depends on timing and is not asserted.
	totalPopped := poppedCount.Load()
	totalSwept := sweptCount.Load()
	if totalPopped+totalSwept > uint64(n) {
		t.Fatalf("over-accounted keys: popped=%d, swept=%d, total=%d > %d", totalPopped, totalSwept, totalPopped+totalSwept, n)
	}
	if l := gc.Len(); l != 0 {
		t.Fatalf("Len = %d after every key was popped or swept, want 0", l)
	}
}

func testE2E_Combination_ConcurrentExtendExpireAndGetRefresh(t *testing.T) {
	t.Parallel()
	gc := gache.New[int](gache.WithDefaultExpiration[int](2 * time.Second))
	const keyCount = 40
	for i := range keyCount {
		gc.Set(fmt.Sprintf("refresh_key_%d", i), i)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			idx := 0
			for {
				select {
				case <-stop:
					return
				default:
					k := fmt.Sprintf("refresh_key_%d", idx%keyCount)
					switch idx % 3 {
					case 0:
						gc.ExtendExpire(k, 100*time.Millisecond)
					case 1:
						_, _ = gc.GetRefresh(k)
					case 2:
						_, _ = gc.GetRefreshWithDur(k, 2*time.Second)
					}
					idx++
				}
			}
		})
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Entries should still be alive because they were constantly refreshed
	for i := range keyCount {
		k := fmt.Sprintf("refresh_key_%d", i)
		if _, ok := gc.Get(k); !ok {
			t.Fatalf("expected key %s to stay alive under continuous refresh", k)
		}
	}
}

func testE2E_Combination_ConcurrentSetIfNotExistsContention(t *testing.T) {
	t.Parallel()
	gc := gache.New[string]()
	const workers = 60
	const targetKey = "exclusive_singleton"

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup

	for i := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier

			// Only the first one to insert will set the value; others must observe existing
			gc.SetIfNotExists(targetKey, fmt.Sprintf("worker_%d", workerID))
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if _, ok := gc.Get(targetKey); !ok {
		t.Fatalf("expected key %s to exist", targetKey)
	}

	// Verify exactly 1 entry exists
	if gc.Len() != 1 {
		t.Fatalf("expected Len=1, got %d", gc.Len())
	}
}

// ============================================================================
// Real-world scenarios
// ============================================================================

// TestE2E_RealWorld implements production-like workloads: a session store, a
// high concurrency cache, a snapshot migration, a sliding-window rate limiter and a
// lock table.
func TestE2E_RealWorld(t *testing.T) {
	t.Parallel()

	t.Run("SessionStoreWithRapidTTLChurn", testE2E_RealWorld_SessionStoreWithRapidTTLChurn)
	t.Run("HighConcurrencyCacheSimulator", testE2E_RealWorld_HighConcurrencyCacheSimulator)
	t.Run("BulkSnapshotMigration", testE2E_RealWorld_BulkSnapshotMigration)
	t.Run("SlidingWindowKeyRotation", testE2E_RealWorld_SlidingWindowKeyRotation)
	t.Run("DistributedCoordinatorLockMap", testE2E_RealWorld_DistributedCoordinatorLockMap)
}

// Scenario 1: High-throughput session store with rapid TTL churn and background daemon sweep.
func testE2E_RealWorld_SessionStoreWithRapidTTLChurn(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var expiredHookCounter atomic.Int64
	gc := gache.New[string]().
		SetExpiredHook(func(ctx context.Context, key string, val string) {
			expiredHookCounter.Add(1)
		}).
		EnableExpiredHook().
		StartExpired(ctx, 20*time.Millisecond)
	t.Cleanup(func() { gc.Stop() })

	const numSessions = 200
	// Active sessions (0..99) start with a TTL far beyond the assertion point (5s:
	// a keeper goroutine starved for longer than a short TTL under -race load let
	// them expire) and are periodically refreshed. Churn sessions (100..199) start
	// with a rapid TTL (40ms) and are abandoned.
	const activeTTL = 5 * time.Second
	for i := range 100 {
		sessionKey := fmt.Sprintf("sess_%04d", i)
		gc.SetWithExpire(sessionKey, fmt.Sprintf("user_%d", i), activeTTL)
	}
	for i := 100; i < numSessions; i++ {
		sessionKey := fmt.Sprintf("sess_%04d", i)
		gc.SetWithExpire(sessionKey, fmt.Sprintf("user_%d", i), 40*time.Millisecond)
	}

	// Keep active sessions refreshed
	stopKeeper := make(chan struct{})
	stopKeeperOnce := sync.OnceFunc(func() { close(stopKeeper) })
	t.Cleanup(stopKeeperOnce) // a Fatal below must not leave the keeper refreshing
	go func() {
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopKeeper:
				return
			case <-ticker.C:
				for i := range 100 {
					_, _ = gc.GetRefreshWithDur(fmt.Sprintf("sess_%04d", i), activeTTL)
				}
			}
		}
	}()

	// Wait (on the cache clock) for abandoned churn sessions (100..199) to expire and be swept by daemon
	gache.WaitCacheElapsed(t, 120*time.Millisecond)

	// Verify surviving active sessions while keeper is still active
	for i := range 100 {
		sessionKey := fmt.Sprintf("sess_%04d", i)
		if _, ok := gc.Get(sessionKey); !ok {
			t.Fatalf("expected refreshed session %s to remain active", sessionKey)
		}
	}

	stopKeeperOnce()

	// Verify unrefreshed churn sessions were swept or expired
	for i := 100; i < numSessions; i++ {
		sessionKey := fmt.Sprintf("sess_%04d", i)
		if _, ok := gc.Get(sessionKey); ok {
			t.Fatalf("expected abandoned session %s to have expired", sessionKey)
		}
	}

	gc.Stop()
}

// Scenario 2: High-concurrency cache simulator (80% read, 15% write, 5% delete/pop).
func testE2E_RealWorld_HighConcurrencyCacheSimulator(t *testing.T) {
	t.Parallel()
	gc := gache.New[int]()
	const initialKeys = 200
	for i := range initialKeys {
		gc.Set(fmt.Sprintf("item_%d", i), i*2)
	}

	const numWorkers = 16
	const opsPerWorker = 500
	var wg sync.WaitGroup

	for w := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			seed := int64(workerID * 9973)
			for range opsPerWorker {
				seed = (seed*1103515245 + 12345) & 0x7fffffff
				roll := seed % 100
				keyIdx := (seed >> 8) % initialKeys
				key := fmt.Sprintf("item_%d", keyIdx)

				switch {
				case roll < 80: // 80% Read / Refresh
					if roll%2 == 0 {
						_, _ = gc.Get(key)
					} else {
						_, _ = gc.GetRefresh(key)
					}
				case roll < 95: // 15% Write
					gc.Set(key, int(seed%10000))
				default: // 5% Delete or Pop
					if roll%2 == 0 {
						_, _ = gc.Delete(key)
					} else {
						_, _ = gc.Pop(key)
					}
				}
			}
		}(w)
	}

	wg.Wait()

	// Validate cache state is self-consistent
	currentLen := gc.Len()
	if currentLen < 0 || currentLen > initialKeys {
		t.Fatalf("unexpected Len=%d outside [0, %d]", currentLen, initialKeys)
	}
	if gc.Size() == 0 {
		t.Fatalf("expected Size() > 0")
	}
}

// Scenario 3: Bulk snapshot migration (Dump & Restore via Read/Write).
func testE2E_RealWorld_BulkSnapshotMigration(t *testing.T) {
	t.Parallel()
	srcCache := gache.New[string]()
	const entryCount = 250
	for i := range entryCount {
		srcCache.Set(fmt.Sprintf("migration_key_%04d", i), fmt.Sprintf("record_value_%04d", i))
	}

	// Serialize to memory buffer
	var snapshot bytes.Buffer
	if err := srcCache.Write(t.Context(), &snapshot); err != nil {
		t.Fatalf("snapshot Write failed: %v", err)
	}

	// Wipe source cache to prove independence
	srcCache.Clear()
	if srcCache.Len() != 0 {
		t.Fatalf("expected srcCache to be empty after Clear")
	}

	// Restore into fresh destination cache
	dstCache := gache.New[string]()
	if err := dstCache.Read(&snapshot); err != nil {
		t.Fatalf("snapshot Read failed: %v", err)
	}

	if dstCache.Len() != entryCount {
		t.Fatalf("expected dstCache Len=%d, got %d", entryCount, dstCache.Len())
	}

	// Verify every record was restored faithfully
	for i := range entryCount {
		k := fmt.Sprintf("migration_key_%04d", i)
		want := fmt.Sprintf("record_value_%04d", i)
		if got, ok := dstCache.Get(k); !ok || got != want {
			t.Fatalf("key %s: got (%q, %v), want %q", k, got, ok, want)
		}
	}
}

// Scenario 4: Sliding-window key rotation and rate limiter state tracking.
func testE2E_RealWorld_SlidingWindowKeyRotation(t *testing.T) {
	t.Parallel()
	gc := gache.New[int]()
	const tokens = 50

	// Populate tokens with a 2s sliding window. The window is wide enough that
	// the extension rounds below land before it closes even when this goroutine
	// is starved under -race load (a 500ms window let even tokens expire before
	// their first ExtendExpire).
	const window = 2 * time.Second
	for i := range tokens {
		gc.SetWithExpire(fmt.Sprintf("rate_token_%d", i), 1, window)
	}
	start := gache.CacheNow()

	// Actively touch even tokens (0, 2, 4...) using ExtendExpire
	// (even deadline: window + 3*300ms)
	for round := 1; round <= 3; round++ {
		gache.WaitCacheClockPast(t, start+int64(round)*int64(40*time.Millisecond))
		for i := 0; i < tokens; i += 2 {
			gc.ExtendExpire(fmt.Sprintf("rate_token_%d", i), 300*time.Millisecond)
		}
	}

	// Wait so un-extended odd tokens expire past the original window
	gache.WaitCacheClockPast(t, start+int64(window))

	// Even tokens must be active
	for i := 0; i < tokens; i += 2 {
		k := fmt.Sprintf("rate_token_%d", i)
		if _, ok := gc.Get(k); !ok {
			t.Fatalf("expected actively extended token %s to be alive", k)
		}
	}

	// Odd tokens must have expired
	for i := 1; i < tokens; i += 2 {
		k := fmt.Sprintf("rate_token_%d", i)
		if _, ok := gc.Get(k); ok {
			t.Fatalf("expected untouched token %s to have expired", k)
		}
	}
}

// Scenario 5: Concurrent gache.Map[K, V] lock manager / coordinator table.
func testE2E_RealWorld_DistributedCoordinatorLockMap(t *testing.T) {
	t.Parallel()
	var lockTable gache.Map[string, string]
	const resources = 10
	const contendersPerResource = 8

	var successfulClaims atomic.Int64
	var lockCollisions atomic.Int64
	var wg sync.WaitGroup

	for r := range resources {
		resourceName := fmt.Sprintf("resource_%d", r)
		var resourceAttempts sync.WaitGroup
		resourceAttempts.Add(contendersPerResource)
		startBarrier := make(chan struct{})

		for c := range contendersPerResource {
			wg.Add(1)
			contenderID := fmt.Sprintf("node_%d_%d", r, c)
			go func(res, node string) {
				defer wg.Done()
				<-startBarrier

				// Attempt to acquire lock via LoadOrStore
				actual, loaded := lockTable.LoadOrStore(res, node)
				if !loaded {
					// Lock acquired!
					successfulClaims.Add(1)
					resourceAttempts.Done()

					// Wait until all sibling contenders on this resource have attempted acquisition
					resourceAttempts.Wait()

					// Release lock via CompareAndDelete
					if !lockTable.CompareAndDelete(res, node) {
						t.Errorf("failed to release lock owned by %s", node)
					}
				} else {
					// Lock already held by someone else
					lockCollisions.Add(1)
					if actual == node {
						t.Errorf("contender %s observed itself as existing owner without acquiring", node)
					}
					resourceAttempts.Done()
				}
			}(resourceName, contenderID)
		}
		close(startBarrier)
	}

	wg.Wait()

	if successfulClaims.Load() != resources {
		t.Fatalf("expected exactly %d successful lock acquisitions, got %d", resources, successfulClaims.Load())
	}
	expectedCollisions := resources * (contendersPerResource - 1)
	if lockCollisions.Load() != int64(expectedCollisions) {
		t.Fatalf("expected %d lock collisions, got %d", expectedCollisions, lockCollisions.Load())
	}
	if lockTable.Len() != 0 {
		t.Fatalf("expected all locks to be released, remaining=%d", lockTable.Len())
	}
}
