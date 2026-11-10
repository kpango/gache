package gache

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"math"
	"math/rand"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// White-box tests of the gache layer: the expiry daemon, the expired-hook queue,
// the collective-operation plumbing (fanOut, gatherDirect, flatten) and the node
// locks, all of which need unexported state.

// ---- expiry daemon and hook server ----

// EnableExpiredHook without SetExpiredHook leaves no function to call. The daemon
// must drop the queued entries instead of calling a nil func, which panics in a
// goroutine and takes the whole process down.
func TestGache_EnableExpiredHookWithoutHook(t *testing.T) {
	t.Parallel()

	const n = 20
	g := New[string]().EnableExpiredHook().(*gache[string])
	g.StartExpired(context.Background(), time.Millisecond)
	t.Cleanup(g.Stop)

	for i := range n {
		g.SetWithExpire(fmt.Sprintf("get-%d", i), "v", time.Millisecond)
	}
	waitCacheElapsed(t, time.Millisecond)
	for i := range n {
		// Each Get that finds its entry expired evicts it and queues an event.
		if v, ok := g.Get(fmt.Sprintf("get-%d", i)); ok {
			t.Errorf("Get of an expired entry = (%q, true)", v)
		}
	}
	waitUntil(t, "the daemon to drain expChan", func() bool { return len(g.expChan) == 0 })

	// The daemon is still alive: it keeps sweeping.
	for i := range n {
		g.SetWithExpire(fmt.Sprintf("sweep-%d", i), "v", time.Millisecond)
	}
	waitCacheElapsed(t, time.Millisecond)
	waitUntil(t, "the daemon to sweep the expired entries", func() bool { return g.Len() == 0 })
}

// StartExpired must hand its cancel func to Stop before it returns, so that a Stop
// right after it reaches the daemon.
func TestGache_StopRightAfterStartExpired(t *testing.T) {
	t.Parallel()

	const instances = 64
	live := make(chan struct{}, instances)
	for range instances {
		g := New[string](WithExpiredHookFunc[string](func(ctx context.Context, _, _ string) {
			if ctx.Err() == nil {
				live <- struct{}{}
			}
		})).(*gache[string])
		g.StartExpired(context.Background(), time.Hour)
		if g.cancel.Load() == nil {
			t.Error("StartExpired returned before it stored the cancel func Stop needs")
		}
		g.Stop()
		// A daemon that is still running consumes this entry and runs the hook with
		// a live context; one that was stopped never does (and a daemon that is
		// still unwinding runs it with a cancelled context).
		g.expChan <- kv[string]{key: "k", value: "v"}
	}

	select {
	case <-live:
		t.Error("an expired hook ran with a live context after Stop: the daemon was not stopped")
	case <-time.After(100 * time.Millisecond):
	}
}

// A second StartExpired stops the daemon the first one started: a cache has one
// daemon, and Stop reaches it.
func TestGache_StartExpiredAgainStopsPreviousDaemon(t *testing.T) {
	t.Parallel()

	ctxs := make(chan context.Context, 1)
	g := New[string](WithExpiredHookFunc[string](func(ctx context.Context, _, _ string) {
		ctxs <- ctx
	})).(*gache[string])
	g.StartExpired(context.Background(), time.Hour)
	t.Cleanup(g.Stop)

	// The first daemon runs the hook; its context is the daemon's.
	g.expChan <- kv[string]{key: "k", value: "v"}
	var first context.Context
	select {
	case first = <-ctxs:
	case <-time.After(daemonWaitTimeout):
		t.Fatal("the first daemon did not run the expired hook")
	}

	g.StartExpired(context.Background(), time.Hour)
	select {
	case <-first.Done():
	case <-time.After(daemonWaitTimeout):
		t.Fatal("the first daemon is still running after a second StartExpired")
	}
}

// The daemon must expire entries and run every hook for any WithMaxWorkers value,
// including a batch that overflows expChan: the sweep then blocks on the full
// channel until the hook side drains it, so the two must never wait for each
// other (even with one worker, the sweep must not hold the slot the hook side
// needs).
func TestGache_DaemonExpiresManyEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option[int]
	}{
		{name: "default"},
		{name: "WithMaxWorkers(0)", opts: []Option[int]{WithMaxWorkers[int](0)}},
		{name: "WithMaxWorkers(-1)", opts: []Option[int]{WithMaxWorkers[int](-1)}},
		{name: "WithMaxWorkers(1)", opts: []Option[int]{WithMaxWorkers[int](1)}},
		{name: "WithMaxWorkers(2)", opts: []Option[int]{WithMaxWorkers[int](2)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				calls atomic.Int64
				done  = make(chan struct{})
				total int64
			)
			opts := append([]Option[int]{WithExpiredHookFunc[int](func(context.Context, string, int) {
				if calls.Add(1) == total {
					close(done)
				}
			})}, tt.opts...)
			g := New[int](opts...).(*gache[int])

			// More expired entries than expChan holds, all expired before the daemon
			// starts, so its first sweep finds them at once.
			total = int64(cap(g.expChan) + 5000)
			for i := range total {
				g.SetWithExpire(fmt.Sprintf("k-%d", i), int(i), time.Millisecond)
			}
			waitCacheElapsed(t, time.Millisecond)

			g.StartExpired(context.Background(), time.Millisecond)
			t.Cleanup(g.Stop)

			// Fail on a stall (no hook ran for daemonWaitTimeout), not on a slow
			// run: under -race on a loaded runner draining all hooks can take longer
			// than that in total.
			waitUntilProgress(t, fmt.Sprintf("all %d expired hooks (Len = %d)", total, g.Len()),
				func() bool {
					select {
					case <-done:
						return true
					default:
						return false
					}
				},
				func() int { return int(calls.Load()) })
			if got := g.Len(); got != 0 {
				t.Errorf("Len = %d after all %d hooks ran, want 0", got, total)
			}
		})
	}
}

// WithMaxWorkers(n <= 0) lifts the limit: operations use runtime.GOMAXPROCS(0)
// workers.
func TestGache_NumWorkers(t *testing.T) {
	t.Parallel()

	procs := runtime.GOMAXPROCS(0)
	tests := []struct {
		name string
		opts []Option[int]
		want int
	}{
		{name: "default", want: min(procs, 2*runtime.NumCPU())},
		{name: "0", opts: []Option[int]{WithMaxWorkers[int](0)}, want: procs},
		{name: "-1", opts: []Option[int]{WithMaxWorkers[int](-1)}, want: procs},
		{name: "min int", opts: []Option[int]{WithMaxWorkers[int](-1 << 63)}, want: procs},
		{name: "1", opts: []Option[int]{WithMaxWorkers[int](1)}, want: 1},
		{name: "GOMAXPROCS", opts: []Option[int]{WithMaxWorkers[int](procs)}, want: procs},
		{name: "above GOMAXPROCS", opts: []Option[int]{WithMaxWorkers[int](procs + 100)}, want: procs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := New[int](tt.opts...).(*gache[int])
			if got := g.numWorkers(); got != tt.want {
				t.Errorf("numWorkers = %d, want %d (GOMAXPROCS %d)", got, tt.want, procs)
			}
		})
	}
}

// The daemon runs twice the fan-out workers of hooks at once, capped by a
// positive WithMaxWorkers, and never fewer than one: errgroup.SetLimit(0) would
// block the daemon for good.
func TestGache_HookWorkers(t *testing.T) {
	t.Parallel()

	procs := runtime.GOMAXPROCS(0)
	tests := []struct {
		name string
		opts []Option[int]
		want int
	}{
		{name: "default", want: min(2*procs, 2*runtime.NumCPU())},
		{name: "0", opts: []Option[int]{WithMaxWorkers[int](0)}, want: 2 * procs},
		{name: "-1", opts: []Option[int]{WithMaxWorkers[int](-1)}, want: 2 * procs},
		{name: "1", opts: []Option[int]{WithMaxWorkers[int](1)}, want: 1},
		{name: "2", opts: []Option[int]{WithMaxWorkers[int](2)}, want: min(2*procs, 2)},
		{name: "above 2*GOMAXPROCS", opts: []Option[int]{WithMaxWorkers[int](2*procs + 100)}, want: 2 * procs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := New[int](tt.opts...).(*gache[int])
			if got := g.hookWorkers(); got != tt.want {
				t.Errorf("hookWorkers = %d, want %d (GOMAXPROCS %d)", got, tt.want, procs)
			}
		})
	}
}

// DeleteExpired blocked on a full expChan (nobody drains it here) must give up
// when its context is cancelled, so a stopping daemon never leaves a sweep behind.
func TestGache_DeleteExpiredGivesUpOnFullQueueWhenCancelled(t *testing.T) {
	t.Parallel()

	g := newHookCache()
	total := cap(g.expChan) + 10
	for i := range total {
		g.SetWithExpire(fmt.Sprintf("k-%d", i), "v", time.Millisecond)
	}
	waitCacheElapsed(t, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deleted := make(chan uint64, 1)
	go func() { deleted <- g.DeleteExpired(ctx) }()

	waitUntil(t, "the queue to fill up", func() bool { return len(g.expChan) == cap(g.expChan) })
	cancel()
	select {
	case n := <-deleted:
		if n < uint64(cap(g.expChan)) {
			t.Errorf("DeleteExpired removed %d entries, want at least %d", n, cap(g.expChan))
		}
	case <-time.After(daemonWaitTimeout):
		t.Fatal("DeleteExpired is still blocked on the full queue after its context was cancelled")
	}
}

// SetExpiredHook may run while the daemon serves events: every event runs exactly
// one of the installed hooks. Under -race a plain func field is a data race.
func TestGache_SetExpiredHookWhileDaemonServes(t *testing.T) {
	t.Parallel()

	const events = 2000
	var calls atomic.Int64
	hook := func(context.Context, string, string) { calls.Add(1) }
	g := New[string](WithExpiredHookFunc[string](hook)).(*gache[string])
	g.StartExpired(context.Background(), time.Hour)
	t.Cleanup(g.Stop)

	stop := make(chan struct{})
	stopped := make(chan struct{})
	// Registered before any t.Fatal path can leave the goroutine running.
	t.Cleanup(func() { close(stop); <-stopped })
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
				g.SetExpiredHook(func(context.Context, string, string) { calls.Add(1) })
			}
		}
	}()
	for i := range events {
		g.expChan <- kv[string]{key: fmt.Sprint("k", i), value: "v"}
	}
	waitUntil(t, "every event to run a hook", func() bool { return calls.Load() == events })
}

// A hook set later replaces the earlier one for the events the daemon serves from
// then on, and SetExpiredHook(nil) leaves no hook to call.
func TestGache_SetExpiredHookReplacesAndClears(t *testing.T) {
	t.Parallel()

	var first, second atomic.Int64
	g := New[string](WithExpiredHookFunc[string](func(context.Context, string, string) { first.Add(1) })).(*gache[string])
	g.StartExpired(context.Background(), time.Hour)
	t.Cleanup(g.Stop)

	g.expChan <- kv[string]{key: "a", value: "v"}
	waitUntil(t, "the first hook", func() bool { return first.Load() == 1 })

	g.SetExpiredHook(func(context.Context, string, string) { second.Add(1) })
	g.expChan <- kv[string]{key: "b", value: "v"}
	waitUntil(t, "the second hook", func() bool { return second.Load() == 1 })

	g.SetExpiredHook(nil)
	g.expChan <- kv[string]{key: "c", value: "v"}
	waitUntil(t, "the daemon to take the event", func() bool { return len(g.expChan) == 0 })
	if first.Load() != 1 || second.Load() != 1 {
		t.Errorf("hooks ran after SetExpiredHook(nil): first = %d, second = %d, want 1 and 1", first.Load(), second.Load())
	}
}

// StartExpired with an interval <= 0 starts no periodic sweeper (time.NewTicker
// panics for it) but still runs the hook server, and it replaces the daemon an
// earlier call started like any other StartExpired.
func TestGache_StartExpiredNonPositiveInterval(t *testing.T) {
	t.Parallel()

	for _, dur := range []time.Duration{0, -1, -time.Second, math.MinInt64} {
		t.Run(dur.String(), func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int64
			g := New[string](WithExpiredHookFunc[string](func(context.Context, string, string) {
				calls.Add(1)
			})).(*gache[string])
			// A daemon that sweeps every millisecond; the call below replaces it.
			g.StartExpired(context.Background(), time.Millisecond)
			t.Cleanup(g.Stop)
			if got := g.StartExpired(context.Background(), dur); got != Gache[string](g) {
				t.Fatal("StartExpired did not return the receiver")
			}

			const n = 20
			for i := range n {
				g.SetWithExpire(fmt.Sprintf("k-%d", i), "v", time.Millisecond)
			}
			waitCacheElapsed(t, time.Millisecond)
			// No sweeper: the expired entries stay until a read finds them.
			time.Sleep(50 * time.Millisecond)
			if got := g.Len(); got != n {
				t.Fatalf("Len = %d, want %d: nothing sweeps", got, n)
			}

			// The hook server runs: the eviction by a read reaches the hook.
			if _, ok := g.Get("k-0"); ok {
				t.Fatal("Get of an expired entry returned true")
			}
			waitUntil(t, "the hook server to run the hook", func() bool { return calls.Load() == 1 })
		})
	}
}

// ---- late expiry evictions vs fresh writes ----

// A reader that sees an expired entry evicts it with g.expiration(key) a moment
// later (get, loop, ExtendExpire and GetRefreshWithDur all do). A Set that lands in
// between replaced the expired value with a fresh one, and the late eviction must
// not take that fresh value away or report it to the expired hook. These tests
// replay the interleaving by calling expiration after the overwrite.

// expiredCache returns a hook cache holding key "k" with value "stale" whose TTL
// has passed on the cache clock. With promote set, the node lives in the read
// snapshot, otherwise only in the dirty map.
func expiredCache(t *testing.T, promote bool) *gache[string] {
	t.Helper()

	g := newHookCache()
	g.SetWithExpire("k", "stale", time.Millisecond)
	waitCacheElapsed(t, time.Millisecond)
	if promote {
		shard, _ := g.shard("k")
		shard.readSnapshot()
	}
	return g
}

func TestGache_Expiration_SparesFreshWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fresh func(t *testing.T, g *gache[string])
		name  string
	}{
		{name: "overwritten by Set", fresh: func(_ *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "fresh", time.Hour)
		}},
		{name: "Set again after Delete", fresh: func(t *testing.T, g *gache[string]) {
			if v, ok := g.Delete("k"); !ok || v != "stale" {
				t.Fatalf("Delete = (%q, %v), want (stale, true)", v, ok)
			}
			g.SetWithExpire("k", "fresh", time.Hour)
		}},
		{name: "overwritten by NoTTL Set", fresh: func(_ *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "fresh", NoTTL)
		}},
	}
	for _, promote := range []bool{false, true} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/promote=%v", tt.name, promote), func(t *testing.T) {
				t.Parallel()

				g := expiredCache(t, promote)
				tt.fresh(t, g)
				g.expiration("k")

				if v, ok := g.Get("k"); !ok || v != "fresh" {
					t.Errorf("Get after a late expiration = (%q, %v), want (fresh, true): the fresh write was evicted", v, ok)
				}
				if got := g.Len(); got != 1 {
					t.Errorf("Len = %d, want 1", got)
				}
				if ev, ok := evicted(g); ok {
					t.Errorf("expired hook got %+v for a value that is not expired", ev)
				}
			})
		}
	}
}

func TestGache_Expiration_EvictsStillExpired(t *testing.T) {
	t.Parallel()

	for _, promote := range []bool{false, true} {
		t.Run(fmt.Sprintf("promote=%v", promote), func(t *testing.T) {
			t.Parallel()

			g := expiredCache(t, promote)
			g.expiration("k")
			// A second, late eviction of the same observation finds nothing to do.
			g.expiration("k")

			if v, ok := g.GetWithIgnoredExpire("k"); ok {
				t.Errorf("expired entry survived: %q", v)
			}
			if got := g.Len(); got != 0 {
				t.Errorf("Len = %d, want 0", got)
			}
			if ev, ok := evicted(g); !ok || ev.key != "k" || ev.value != "stale" {
				t.Errorf("expired hook got (%+v, %v), want key k value stale", ev, ok)
			}
			if ev, ok := evicted(g); ok {
				t.Errorf("expired hook got a second event %+v for one eviction", ev)
			}
		})
	}
}

// Readers hammer keys whose entry is expired while a writer replaces each expired
// value with a fresh long-lived one. Whatever a reader evicts must be an expired
// value: a fresh write may never disappear, nor reach the expired hook.
func TestGache_ExpiredReadersNeverEvictFreshWrites(t *testing.T) {
	t.Parallel()

	const (
		keys    = 4
		readers = 4 // enough to expose an eviction that takes a fresh write: hundreds are lost per run
		rounds  = 20000
	)
	g := newHookCache()
	names := make([]string, keys)
	for i := range names {
		names[i] = fmt.Sprintf("key-%d", i)
	}

	var freshEvents atomic.Int64
	drained := make(chan struct{})
	stopDrain := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			select {
			case ev := <-g.expChan:
				if strings.HasPrefix(ev.value, "fresh") {
					freshEvents.Add(1)
				}
			case <-stopDrain:
				return
			}
		}
	}()

	var (
		stop sync.WaitGroup
		quit atomic.Bool
	)
	for range readers {
		stop.Go(func() {
			for i := 0; !quit.Load(); i++ {
				g.Get(names[i%keys])
			}
		})
	}

	var (
		writers sync.WaitGroup
		lost    atomic.Int64
	)
	for _, key := range names {
		writers.Go(func() {
			shard, h := g.shard(key)
			for i := range rounds {
				g.SetWithExpire(key, "stale", time.Hour)
				// Back-date the entry: readers now see it expired and race to evict it.
				if n, ok := shard.loadNode(h, key); ok {
					n.exp().Store(1)
				}
				fresh := fmt.Sprintf("fresh-%d", i)
				g.SetWithExpire(key, fresh, time.Hour)
				if v, ok := g.Get(key); !ok || v != fresh {
					lost.Add(1)
				}
			}
		})
	}
	writers.Wait()
	quit.Store(true)
	stop.Wait()
	close(stopDrain)
	<-drained

	if n := lost.Load(); n != 0 {
		t.Errorf("%d of %d fresh writes were evicted by a reader that had seen the previous value expired", n, keys*rounds)
	}
	if n := freshEvents.Load(); n != 0 {
		t.Errorf("%d fresh values were reported to the expired hook", n)
	}
}

// ---- expired-hook queue ----

// mustReturn runs f on its own goroutine and waits for it with waitUntilProgress:
// it fails only if f has not returned and progress (the count of entries still to
// evict) has not changed for daemonWaitTimeout, so a call stuck on a full expired
// hook queue fails the test instead of hanging the package. The limit applies to a
// stall, not to the whole call, which under -race on a loaded runner can take
// longer (a fixed deadline on the whole call failed spuriously with -count=3).
func mustReturn(tb testing.TB, what string, f func(), progress func() int) {
	tb.Helper()
	var done atomic.Bool
	go func() {
		defer done.Store(true)
		f()
	}()
	waitUntilProgress(tb, what+" to return without blocking on the expired hook queue", done.Load, progress)
}

type readPath struct {
	run  func(g *gache[string], keys []string)
	name string
}

// readPaths are the operations that evict an expired entry they find and report it
// to the expired hook (see expiration and Pop).
func readPaths() []readPath {
	return []readPath{
		{name: "Get", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.Get(k)
			}
		}},
		{name: "GetWithExpire", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.GetWithExpire(k)
			}
		}},
		{name: "GetRefresh", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.GetRefresh(k)
			}
		}},
		{name: "GetRefreshWithDur", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.GetRefreshWithDur(k, time.Minute)
			}
		}},
		{name: "ExtendExpire", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.ExtendExpire(k, time.Minute)
			}
		}},
		{name: "Pop", run: func(g *gache[string], keys []string) {
			for _, k := range keys {
				g.Pop(k)
			}
		}},
		{name: "Range", run: func(g *gache[string], _ []string) {
			g.Range(context.Background(), func(string, string, int64) bool { return true })
		}},
		{name: "ToMap", run: func(g *gache[string], _ []string) {
			g.ToMap(context.Background())
		}},
	}
}

// Every read path that finds an expired entry evicts it and then offers it to the
// hook queue without waiting: when the queue is full the notification is dropped
// and the entry is still evicted. The queue is full and nothing drains it when no
// daemon runs, after Stop, and while every hook worker is stuck in a slow hook.
func TestGache_ReadPathsNeverBlockOnFullHookQueue(t *testing.T) {
	t.Parallel()

	states := []struct {
		setup func(t *testing.T, g *gache[string])
		name  string
		full  bool // nobody can drain the queue, so it ends up exactly full
	}{
		{name: "no daemon", full: true, setup: func(*testing.T, *gache[string]) {}},
		{name: "after Stop", setup: func(_ *testing.T, g *gache[string]) {
			g.StartExpired(context.Background(), time.Hour)
			g.Stop()
		}},
		{name: "slow hook", setup: func(t *testing.T, g *gache[string]) {
			t.Helper()
			release := make(chan struct{})
			g.SetExpiredHook(func(context.Context, string, string) { <-release })
			t.Cleanup(g.Stop)
			t.Cleanup(func() { close(release) })
			g.StartExpired(context.Background(), time.Hour)
		}},
	}
	for _, st := range states {
		for _, op := range readPaths() {
			t.Run(st.name+"/"+op.name, func(t *testing.T) {
				t.Parallel()

				g := newHookCache()
				st.setup(t, g)

				// More expired entries than the queue and every hook worker can hold.
				total := cap(g.expChan) + g.hookWorkers() + 1000
				keys := make([]string, total)
				for i := range keys {
					keys[i] = fmt.Sprintf("k-%d", i)
					g.SetWithExpire(keys[i], "v", time.Millisecond)
				}
				waitCacheElapsed(t, time.Millisecond)

				mustReturn(t, op.name, func() { op.run(g, keys) }, g.Len)

				if got := g.Len(); got != 0 {
					t.Errorf("Len = %d after %s, want 0: expired entries must be evicted although their notification is dropped", got, op.name)
				}
				if st.full && len(g.expChan) != cap(g.expChan) {
					t.Errorf("queue holds %d of %d events, want it full", len(g.expChan), cap(g.expChan))
				}
			})
		}
	}
}

// A hook that reads the cache re-enters the read paths while the daemon's sweep
// waits for room in the queue the hooks drain. The reads must neither wait for
// that room nor stall the hooks: the daemon expires every entry. With one hook
// worker the stall is certain: the hook waits for room, the sweep waits for room,
// and the only receiver waits for the hook.
func TestGache_ReentrantHookDoesNotStallDaemon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option[int]
	}{
		{name: "one hook worker", opts: []Option[int]{WithMaxWorkers[int](1)}},
		{name: "default workers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const related = 4
			var calls atomic.Int64
			var g *gache[int]
			opts := append([]Option[int]{WithExpiredHookFunc[int](func(_ context.Context, k string, _ int) {
				calls.Add(1)
				// Related keys that are expired as well: each Get evicts one.
				for j := range related {
					g.Get(fmt.Sprintf("rel-%s-%d", k, j))
				}
			})}, tt.opts...)
			g = New[int](opts...).(*gache[int])

			// A batch larger than the queue, all expired before the daemon starts.
			n := cap(g.expChan) + 100
			for i := range n {
				k := fmt.Sprint("k", i)
				g.SetWithExpire(k, i, time.Millisecond)
				for j := range related {
					g.SetWithExpire(fmt.Sprintf("rel-%s-%d", k, j), i, time.Millisecond)
				}
			}
			waitCacheElapsed(t, time.Millisecond)

			g.StartExpired(context.Background(), time.Millisecond)
			t.Cleanup(g.Stop)

			waitUntilProgress(t, "the daemon to expire every entry and drain the queue", func() bool {
				return g.Len() == 0 && len(g.expChan) == 0
			}, func() int { return g.Len() - int(calls.Load()) })
			if calls.Load() == 0 {
				t.Error("no expired hook ran")
			}
		})
	}
}

// The queue belongs to the cache: entries queued before any daemon runs, or while
// one has been stopped, stay queued and the next daemon serves every one of them.
func TestGache_QueuedEntriesSurviveStop(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	g := New[string](WithExpiredHookFunc[string](func(context.Context, string, string) {
		calls.Add(1)
	})).(*gache[string])
	if got, want := cap(g.expChan), 10*slen; got != want {
		t.Errorf("queue capacity = %d, want %d (10 per shard)", got, want)
	}

	const batch = 5
	expire := func(prefix string) {
		for i := range batch {
			g.SetWithExpire(fmt.Sprint(prefix, i), "v", time.Millisecond)
		}
		waitCacheElapsed(t, time.Millisecond)
		for i := range batch {
			g.Get(fmt.Sprint(prefix, i))
		}
	}

	expire("before-")
	if got := len(g.expChan); got != batch {
		t.Fatalf("queue holds %d entries before any daemon runs, want %d", got, batch)
	}
	g.StartExpired(context.Background(), time.Hour)
	waitUntil(t, "the first daemon to serve the queued entries", func() bool { return calls.Load() == batch })

	g.Stop()
	expire("after-")
	g.StartExpired(context.Background(), time.Hour)
	t.Cleanup(g.Stop)
	// The stopped daemon may still take an entry off the queue while it winds
	// down; either way every entry is served.
	waitUntil(t, "the next daemon to serve the entries queued after Stop", func() bool { return calls.Load() == 2*batch })
}

// DisableExpiredHook stops queueing; what is queued already stays queued and the
// daemon still serves it.
func TestGache_DisableExpiredHookKeepsQueuedEntries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	g := New[string](WithExpiredHookFunc[string](func(context.Context, string, string) {
		calls.Add(1)
	})).(*gache[string])
	g.SetWithExpire("a", "v", time.Millisecond)
	g.SetWithExpire("b", "v", time.Millisecond)
	waitCacheElapsed(t, time.Millisecond)

	g.Get("a")
	g.DisableExpiredHook()
	g.Get("b")
	if got := len(g.expChan); got != 1 {
		t.Fatalf("queue holds %d entries, want 1: only the eviction before the disable is queued", got)
	}

	g.StartExpired(context.Background(), time.Hour)
	t.Cleanup(g.Stop)
	waitUntil(t, "the daemon to serve the entry queued before the disable", func() bool { return calls.Load() == 1 })
}

// The calls that EnableExpiredHook lists as queueing nothing leave an expired
// entry in place or replace it without a report.
func TestGache_NonEvictingCallsQueueNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tests := []struct {
		call func(t *testing.T, g *gache[string])
		name string
	}{
		{name: "Keys", call: func(_ *testing.T, g *gache[string]) { g.Keys(ctx) }},
		{name: "Values", call: func(_ *testing.T, g *gache[string]) { g.Values(ctx) }},
		{name: "ToRawMap", call: func(_ *testing.T, g *gache[string]) { g.ToRawMap(ctx) }},
		{name: "Write", call: func(t *testing.T, g *gache[string]) {
			if err := g.Write(ctx, io.Discard); err != nil {
				t.Errorf("Write: %v", err)
			}
		}},
		{name: "GetWithIgnoredExpire", call: func(_ *testing.T, g *gache[string]) { g.GetWithIgnoredExpire("k") }},
		{name: "Set", call: func(_ *testing.T, g *gache[string]) { g.Set("k", "new") }},
		{name: "SetIfNotExists", call: func(_ *testing.T, g *gache[string]) { g.SetIfNotExists("k", "new") }},
		{name: "SetWithExpireIfNotExists", call: func(_ *testing.T, g *gache[string]) { g.SetWithExpireIfNotExists("k", "new", time.Hour) }},
		{name: "Delete", call: func(_ *testing.T, g *gache[string]) { g.Delete("k") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newHookCache()
			g.SetWithExpire("k", "old", time.Millisecond)
			waitCacheElapsed(t, time.Millisecond)

			tt.call(t, g)
			if got := len(g.expChan); got != 0 {
				t.Errorf("%s queued %d entries for the expired hook, want none", tt.name, got)
			}
		})
	}
}

// ---- node locks on the refresh and eviction paths ----

// TestGache_NodeLock_ExpiredEntryIsEvicted pins what ExtendExpire,
// GetRefreshWithDur and GetRefresh do with an entry that has expired: it is
// deleted, reported to the expired hook, and neither extended nor returned.
func TestGache_NodeLock_ExpiredEntryIsEvicted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		call func(g *gache[string]) (v string, ok bool)
		name string
	}{
		{name: "ExtendExpire", call: func(g *gache[string]) (string, bool) {
			g.ExtendExpire("k", time.Hour)
			return "", false
		}},
		{name: "GetRefreshWithDur", call: func(g *gache[string]) (string, bool) {
			return g.GetRefreshWithDur("k", time.Hour)
		}},
		{name: "GetRefresh", call: func(g *gache[string]) (string, bool) {
			return g.GetRefresh("k")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newHookCache()
			g.SetWithExpire("k", "v", 20*time.Millisecond)
			waitCacheElapsed(t, 20*time.Millisecond)

			if v, ok := tt.call(g); ok || v != "" {
				t.Errorf("call on an expired entry = (%q, %v), want (%q, false)", v, ok, "")
			}
			if got := g.Len(); got != 0 {
				t.Errorf("Len = %d, want 0: the expired entry was not deleted", got)
			}
			if ev, ok := evicted(g); !ok || ev.key != "k" || ev.value != "v" {
				t.Errorf("expired hook got (%+v, %v), want key k value v", ev, ok)
			}
			if v, ok := g.GetWithIgnoredExpire("k"); ok {
				t.Errorf("entry survived: GetWithIgnoredExpire = %q", v)
			}
		})
	}
}

// TestGache_NodeLock_LiveEntryIsRefreshed pins the update ExtendExpire and
// GetRefreshWithDur apply to a live, unexpired entry.
func TestGache_NodeLock_LiveEntryIsRefreshed(t *testing.T) {
	t.Parallel()

	t.Run("ExtendExpire adds exactly addExp", func(t *testing.T) {
		t.Parallel()

		g := newHookCache()
		g.SetWithExpire("k", "v", time.Hour)
		_, before, _ := g.GetWithExpire("k")
		g.ExtendExpire("k", time.Minute)
		v, after, ok := g.GetWithExpire("k")
		if !ok || v != "v" {
			t.Fatalf("GetWithExpire = (%q, %v), want (v, true)", v, ok)
		}
		if got, want := after-before, int64(time.Minute); got != want {
			t.Errorf("expire grew by %d, want %d", got, want)
		}
		if ev, ok := evicted(g); ok {
			t.Errorf("a live entry reached the expired hook: %+v", ev)
		}
	})

	t.Run("GetRefreshWithDur returns the value and resets the TTL", func(t *testing.T) {
		t.Parallel()

		g := newHookCache()
		g.SetWithExpire("k", "v", time.Hour)
		_, before, _ := g.GetWithExpire("k")
		v, ok := g.GetRefreshWithDur("k", 3*time.Hour)
		if !ok || v != "v" {
			t.Fatalf("GetRefreshWithDur = (%q, %v), want (v, true)", v, ok)
		}
		_, after, _ := g.GetWithExpire("k")
		if after-before < int64(90*time.Minute) {
			t.Errorf("expire moved by %v, want about +2h", time.Duration(after-before))
		}
	})

	t.Run("GetRefreshWithDur on an entry without a deadline", func(t *testing.T) {
		t.Parallel()

		g := newHookCache()
		g.SetWithExpire("k", "v", NoTTL)
		v, ok := g.GetRefreshWithDur("k", time.Hour)
		if !ok || v != "v" {
			t.Fatalf("GetRefreshWithDur = (%q, %v), want (v, true)", v, ok)
		}
		if _, exp, _ := g.GetWithExpire("k"); exp <= cacheNow() {
			t.Errorf("expire = %d, want a deadline in the future", exp)
		}
		if ev, ok := evicted(g); ok {
			t.Errorf("an entry without a deadline reached the expired hook: %+v", ev)
		}
	})

	t.Run("absent key", func(t *testing.T) {
		t.Parallel()

		g := newHookCache()
		g.ExtendExpire("none", time.Minute)
		if v, ok := g.GetRefreshWithDur("none", time.Minute); ok || v != "" {
			t.Errorf("GetRefreshWithDur on an absent key = (%q, %v)", v, ok)
		}
		if got := g.Len(); got != 0 {
			t.Errorf("Len = %d, want 0", got)
		}
	})
}

// TestGache_NodeLock_SetWithExpireIfNotExists pins SetWithExpireIfNotExists
// across node states: absent, tombstoned, live and valid (kept), live without a
// deadline (kept) and expired (replaced in place, without the expired hook).
func TestGache_NodeLock_SetWithExpireIfNotExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prepare    func(t *testing.T, g *gache[string])
		name       string
		wantVal    string
		wantLonger bool // the resulting expire must be later than before
	}{
		{name: "absent", prepare: func(*testing.T, *gache[string]) {}, wantVal: "new"},
		{name: "tombstoned", prepare: func(_ *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "old", time.Hour)
			g.Delete("k")
		}, wantVal: "new"},
		{name: "live and valid is kept", prepare: func(_ *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "old", time.Hour)
		}, wantVal: "old"},
		{name: "live without deadline is kept", prepare: func(_ *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "old", NoTTL)
		}, wantVal: "old"},
		{name: "expired is replaced", prepare: func(t *testing.T, g *gache[string]) {
			g.SetWithExpire("k", "old", 20*time.Millisecond)
			waitCacheElapsed(t, 20*time.Millisecond)
		}, wantVal: "new", wantLonger: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newHookCache()
			tt.prepare(t, g)
			_, before, existed := rawEntry(g, "k")

			g.SetWithExpireIfNotExists("k", "new", 2*time.Hour)

			v, after, ok := g.GetWithExpire("k")
			if !ok || v != tt.wantVal {
				t.Fatalf("GetWithExpire = (%q, %v), want (%q, true)", v, ok, tt.wantVal)
			}
			if existed && !tt.wantLonger && after != before {
				t.Errorf("a kept entry's expire changed: %d -> %d", before, after)
			}
			if tt.wantLonger && after <= before {
				t.Errorf("a replaced entry's expire did not move: %d -> %d", before, after)
			}
			if got := g.Len(); got != 1 {
				t.Errorf("Len = %d, want 1", got)
			}
			if ev, ok := evicted(g); ok {
				t.Errorf("SetWithExpireIfNotExists reached the expired hook: %+v", ev)
			}
		})
	}
}

// rawEntry returns an entry's value and expire without filtering expired entries
// (Get would).
func rawEntry[V any](g *gache[V], key string) (v V, expire int64, ok bool) {
	shard, h := g.shard(key)
	n, found := shard.loadNode(h, key)
	if !found {
		return v, 0, false
	}
	return n.readValue()
}

// ---- gather: flatten, Keys/Values, window overflow ----

// windows carves buf into gatherDirect's windows, window w ending at ends[w].
func windows[T any](buf []T, ends []int) [][]T {
	chunks := make([][]T, len(ends))
	start := 0
	for w, end := range ends {
		chunks[w] = buf[start:start:end]
		start = end
	}
	return chunks
}

func TestGather_Flatten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// ends are the window ends in a buf of ends[len-1] elements; fill[w] is
		// how many elements worker w appends to its window (more than the window
		// holds moves the chunk out of buf).
		ends   []int
		fill   []int
		inBuf  bool // the result must be buf itself
		wantGC bool // the tail of buf past the result must be cleared
	}{
		{name: "full windows stay in place", ends: []int{2, 5, 9}, fill: []int{2, 3, 4}, inBuf: true},
		{name: "short window compacts the ones after it", ends: []int{3, 6, 9}, fill: []int{3, 1, 3}, inBuf: true, wantGC: true},
		{name: "short last window", ends: []int{3, 6, 9}, fill: []int{3, 3, 0}, inBuf: true, wantGC: true},
		{name: "empty window in the middle", ends: []int{2, 2, 5}, fill: []int{2, 0, 3}, inBuf: true},
		{name: "overflowing window moves out of buf", ends: []int{2, 4, 6}, fill: []int{2, 5, 2}},
		{name: "insert into an empty window", ends: []int{2, 2, 4}, fill: []int{2, 1, 2}},
		{name: "overflowing first window", ends: []int{1, 4}, fill: []int{3, 3}},
		{name: "lone window", ends: []int{4}, fill: []int{4}, inBuf: true},
		{name: "lone short window", ends: []int{4}, fill: []int{2}, inBuf: true, wantGC: true},
		{name: "nothing gathered", ends: []int{2, 4}, fill: []int{0, 0}, inBuf: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := make([]*int, tt.ends[len(tt.ends)-1])
			chunks := windows(buf, tt.ends)
			var want []*int
			for w, n := range tt.fill {
				for range n {
					p := new(int)
					*p = len(want)
					chunks[w] = append(chunks[w], p)
					want = append(want, p)
				}
			}

			got := flatten(chunks, buf)
			if got == nil {
				t.Fatal("flatten = nil, want a non-nil slice")
			}
			if !slices.Equal(got, want) {
				t.Fatalf("flatten = %v, want %v", got, want)
			}
			if inBuf := cap(got) != 0 && &got[:1][0] == &buf[0]; inBuf != tt.inBuf {
				t.Errorf("result in buf = %v, want %v", inBuf, tt.inBuf)
			}
			if tt.wantGC {
				for i, p := range buf[len(got):] {
					if p != nil {
						t.Errorf("buf[%d] = %p after the result, want nil", len(got)+i, p)
					}
				}
			}
		})
	}

	t.Run("no chunks", func(t *testing.T) {
		t.Parallel()

		if got := flatten[int](nil, nil); got == nil || len(got) != 0 {
			t.Errorf("flatten(nil, nil) = %#v, want a non-nil empty slice", got)
		}
	})

	t.Run("zero-size elements", func(t *testing.T) {
		t.Parallel()

		buf := make([]struct{}, 3)
		chunks := windows(buf, []int{1, 3})
		chunks[0] = append(chunks[0], struct{}{}, struct{}{}, struct{}{})
		chunks[1] = append(chunks[1], struct{}{})
		if got := flatten(chunks, buf); len(got) != 4 {
			t.Errorf("len(flatten) = %d, want 4", len(got))
		}
	})
}

// Keys and Values return every live entry exactly once when several workers fill
// their windows of buf: an element written into another worker's window would
// show up as a duplicate and a missing one.
func TestGache_KeysValuesExact(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 3, 64} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			t.Parallel()

			g := New[int](WithMaxWorkers[int](workers))
			const n = 20000
			wantKeys := make([]string, 0, n)
			wantVals := make([]int, 0, n)
			for i := range n {
				k := "k" + strconv.Itoa(i)
				g.SetWithExpire(k, i, NoTTL)
				wantKeys = append(wantKeys, k)
				wantVals = append(wantVals, i)
			}
			slices.Sort(wantKeys)
			check := func(when string) {
				t.Helper()
				keys := g.Keys(context.Background())
				slices.Sort(keys)
				if !slices.Equal(keys, wantKeys) {
					t.Errorf("%s: Keys: %d keys, want the %d live ones exactly once", when, len(keys), len(wantKeys))
				}
				vals := g.Values(context.Background())
				slices.Sort(vals)
				if !slices.Equal(vals, wantVals) {
					t.Errorf("%s: Values: %d values, want the %d live ones exactly once", when, len(vals), len(wantVals))
				}
				if raw := g.ToRawMap(context.Background()); len(raw) != len(wantKeys) {
					t.Errorf("%s: ToRawMap: %d entries, want %d", when, len(raw), len(wantKeys))
				}
			}
			check("full windows")

			// Expired entries are counted in the window sizes but not gathered, so
			// windows come out short and flatten compacts them.
			for i := range 500 {
				g.SetWithExpire("gone"+strconv.Itoa(i), -1, 1)
			}
			waitCacheElapsed(t, time.Nanosecond)
			check("short windows")
		})
	}
}

// A worker that finds more entries than the lengths gatherDirect sized its window
// with must still return all of them: with one worker walking the shards in
// order, keys stored from extract while it walks shard 0 land in shards it has not
// walked yet, so its chunk outgrows its window and flatten copies it out of buf.
func TestGatherDirect_WindowOverflow(t *testing.T) {
	t.Parallel()

	g := New[int](WithMaxWorkers[int](1)).(*gache[int])
	const n = 100
	for i := range n {
		g.SetWithExpire("k"+strconv.Itoa(i), i, NoTTL)
	}
	inserted := false
	chunks, buf := gatherDirect(context.Background(), g, false, func(k string, _ int) string {
		if !inserted {
			inserted = true
			for i := range 4 * n {
				g.SetWithExpire("late"+strconv.Itoa(i), i, NoTTL)
			}
		}
		return k
	})
	if len(chunks) != 1 || len(chunks[0]) <= n {
		t.Fatalf("got %d chunks, chunk 0 holding %d keys, want 1 chunk with more than %d", len(chunks), len(chunks[0]), n)
	}
	if cap(chunks[0]) != 0 && &chunks[0][:1][0] == &buf[0] {
		t.Fatal("the chunk is still in buf, want it moved out by append")
	}
	keys := flatten(chunks, buf)
	if len(keys) != len(chunks[0]) {
		t.Fatalf("len(flatten) = %d, want %d", len(keys), len(chunks[0]))
	}
	for i := range n {
		if k := "k" + strconv.Itoa(i); !slices.Contains(keys, k) {
			t.Errorf("key %q missing", k)
		}
	}
	if slices.Contains(keys, "") {
		t.Error("an empty key in the result, want only gathered keys")
	}
}

// ---- fan-out, range workers and shard-level regressions ----

// TestFanOut_CoversEveryShardOnce pins fanOut's contract for every nprocs,
// including more workers than shards (which fanOut clamps itself): each shard is
// handed to exactly one run, the worker indexes are distinct and run from 0 up to
// at most min(nprocs, slen)-1, and the result is the sum of the run results.
func TestFanOut_CoversEveryShardOnce(t *testing.T) {
	t.Parallel()

	for _, nprocs := range []int{1, 2, 3, 7, 64, 1000, slen - 1, slen, slen + 1, 5000, 1 << 20} {
		t.Run(strconv.Itoa(nprocs), func(t *testing.T) {
			t.Parallel()

			var (
				mu      sync.Mutex
				covered [slen]int
				workers []int
			)
			limit := min(nprocs, slen)
			got := fanOut(nprocs, func(worker, start, end int) uint64 {
				mu.Lock()
				defer mu.Unlock()
				workers = append(workers, worker)
				for j := start; j < end; j++ {
					covered[j]++
				}
				return uint64(end - start)
			})

			slices.Sort(workers)
			if len(workers) == 0 || len(workers) > limit {
				t.Fatalf("%d runs, want between 1 and %d", len(workers), limit)
			}
			for i, w := range workers {
				if w != i {
					t.Fatalf("worker indexes %v, want 0..%d each once", workers, len(workers)-1)
				}
			}
			for j, c := range covered {
				if c != 1 {
					t.Fatalf("shard %d covered %d times, want 1", j, c)
				}
			}
			if got != slen {
				t.Errorf("fanOut = %d, want %d", got, slen)
			}
		})
	}
}

// Range calls f from one goroutine per range of shards: returning false stops only
// the goroutine that got it, so f is called again by the others. With
// WithMaxWorkers(1) the walk is sequential and false ends it after exactly the
// visit that returned it; cancelling the context stops the walk too.
func TestGache_RangeFalseStopsOnlyItsWorker(t *testing.T) {
	t.Parallel()

	fill := func(g Gache[int]) {
		for i := range 4096 {
			g.SetWithExpire(fmt.Sprint("k", i), i, NoTTL)
		}
	}

	t.Run("one worker", func(t *testing.T) {
		t.Parallel()

		for _, limit := range []int{1, 5, 10} {
			t.Run(fmt.Sprint("false on visit ", limit), func(t *testing.T) {
				t.Parallel()

				g := New[int](WithMaxWorkers[int](1))
				fill(g)
				visits := 0
				g.Range(context.Background(), func(string, int, int64) bool {
					visits++
					return visits < limit
				})
				if visits != limit {
					t.Errorf("f called %d times, want %d: false must end a sequential walk", visits, limit)
				}
			})
		}
	})

	t.Run("several workers", func(t *testing.T) {
		t.Parallel()

		g := New[int](WithMaxWorkers[int](0))
		workers := int64(g.(*gache[int]).numWorkers())
		if workers < 2 {
			t.Skip("GOMAXPROCS is 1: the walk is sequential")
		}
		fill(g)
		var n atomic.Int64
		g.Range(context.Background(), func(string, int, int64) bool {
			n.Add(1)
			return false
		})
		if calls := n.Load(); calls < 2 || calls > workers {
			t.Errorf("f called %d times, want 2..%d: false stops only its own worker", calls, workers)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		t.Parallel()

		g := New[int](WithMaxWorkers[int](4))
		fill(g)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var visits atomic.Int32
		g.Range(ctx, func(string, int, int64) bool {
			if visits.Add(1) == 10 {
				cancel()
			}
			return true
		})
		if got := visits.Load(); got < 10 || got >= 4096 {
			t.Errorf("f called %d times, want 10 <= n < 4096: cancel must halt the walk", got)
		}
	})
}

// An entry without a TTL has expire <= 0 (SetWithExpire with NoTTL or 0). Writing
// "expire + d" or "now + d" into it, which ExtendExpire and GetRefresh(WithDur)
// must not do, would turn it into an entry that expired in 1970 (or just now).
func TestGache_TTLLessEntrySurvivesRefresh(t *testing.T) {
	t.Parallel()

	// setup stores "k" and returns the expire the entry must keep; op is the call
	// under test and reports whether it found the entry.
	type tc struct {
		name  string
		setup func(g *gache[string]) (wantExpire int64)
		op    func(g *gache[string]) (ok bool)
	}
	var tests []tc
	for _, ttl := range []time.Duration{NoTTL, 0} {
		tests = append(tests,
			// ExtendExpire leaves an entry without TTL alone.
			tc{
				name: "ExtendExpire/" + ttl.String(),
				setup: func(g *gache[string]) int64 {
					g.SetWithExpire("k", "v", ttl)
					_, e, _ := g.GetWithExpire("k")
					return e
				},
				op: func(g *gache[string]) bool { g.ExtendExpire("k", time.Minute); return true },
			},
			// GetRefresh with a no-TTL default keeps the entry without TTL.
			tc{
				name: "GetRefresh/default " + ttl.String(),
				setup: func(g *gache[string]) int64 {
					g.SetDefaultExpire(ttl)
					g.Set("k", "v")
					_, e, _ := g.GetWithExpire("k")
					return e
				},
				op: func(g *gache[string]) bool { _, ok := g.GetRefresh("k"); return ok },
			},
		)
	}
	// GetRefreshWithDur(key, d) with d <= 0 stores d exactly as SetWithExpire does.
	for _, d := range []time.Duration{NoTTL, 0, -5, -time.Hour} {
		tests = append(tests, tc{
			name: "GetRefreshWithDur/" + d.String(),
			setup: func(g *gache[string]) int64 {
				want := New[string]()
				want.SetWithExpire("k", "v", d)
				_, e, _ := want.GetWithExpire("k")
				g.SetWithExpire("k", "v", time.Hour)
				return e
			},
			op: func(g *gache[string]) bool { _, ok := g.GetRefreshWithDur("k", d); return ok },
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newHookCache()
			want := tt.setup(g)
			if !tt.op(g) {
				t.Fatal("the call reported the entry missing")
			}
			waitCacheElapsed(t, 10*time.Millisecond)

			v, got, ok := g.GetWithExpire("k")
			if !ok || v != "v" {
				t.Fatalf("GetWithExpire = (%q, %d, %v), want the entry to survive", v, got, ok)
			}
			if got != want {
				t.Errorf("expire = %d, want %d: the entry must stay without TTL", got, want)
			}
			if ev, ok := evicted(g); ok {
				t.Errorf("expired hook got %+v for an entry without TTL", ev)
			}
		})
	}
}

// Regression: keys colliding on MaxKeyLength share a bucket; a promotion lost an
// in-place update of one of them and resurrected a deleted one.
func TestGache_CollisionLostUpdateAndResurrection(t *testing.T) {
	g := New[string](WithMaxKeyLength[string](8))
	defer g.Stop()

	// Keys with identical 8-byte prefix will produce identical 64-bit hash
	k1 := "prefix__alpha"
	k2 := "prefix__beta"
	k3 := "prefix__gamma"

	g.Set(k1, "v1")
	g.Set(k2, "v2")
	g.Set(k3, "v3")

	// Trigger initial promotion: access k1 which is in dirty but not in read
	for range 20 {
		g.Get(k1)
	}

	// Delete k2 (middle of collision chain)
	if del, ok := g.Delete(k2); !ok || del != "v2" {
		t.Fatalf("failed to delete %s: got (%q, %v)", k2, del, ok)
	}

	// Find a key that lands on the exact same shard as k1 but with a different hash h
	internalGache := g.(*gache[string])
	targetShard, hTarget := internalGache.shard(k1)
	t.Logf("targetShard: %p, hTarget: 0x%x, shardIdx: %d", targetShard, hTarget, hTarget&mask)
	var triggerKey string
	for i := range 100_000 {
		candidate := fmt.Sprintf("%08x", i)
		s, h := internalGache.shard(candidate)
		if s == targetShard && h != hTarget {
			triggerKey = candidate
			break
		}
	}
	if triggerKey == "" {
		t.Fatal("could not find triggering key for the same shard")
	}

	// Trigger dirtyLocked() on targetShard by inserting triggerKey
	g.Set(triggerKey, "trigger_val")

	// In-place update k1
	g.Set(k1, "v1_updated")

	// Delete k3 while dirty map holds an unlinked live copy of k3
	if del, ok := g.Delete(k3); !ok || del != "v3" {
		t.Fatalf("failed to delete %s: got (%q, %v)", k3, del, ok)
	}

	// Trigger promotion again by accessing triggerKey which is in dirty but not in read
	for range 20 {
		g.Get(triggerKey)
	}

	// Check if k1 kept its update
	v, ok := g.Get(k1)
	if !ok {
		t.Fatalf("%s not found after promotion", k1)
	}
	if v != "v1_updated" {
		t.Errorf(" %s update was LOST! Got %q, want %q", k1, v, "v1_updated")
	}

	// Check if k3 resurrected
	v3, ok3 := g.Get(k3)
	if ok3 {
		t.Errorf(" deleted key %s RESURRECTED after promotion! Got %q, ok=true", k3, v3)
	}
}

// The contiguous shard arena is neither collected nor corrupted while instances are
// allocated, mutated and read under continuous runtime.GC().
func TestGache_ShardArenaSurvivesGC(t *testing.T) {
	const numInstances = 1000
	const retainedInstances = 200
	const keysPerInstance = 128

	var stopGC atomic.Bool
	var gcWg sync.WaitGroup

	// Dedicated GC pressure goroutines
	for range 2 {
		gcWg.Go(func() {
			for !stopGC.Load() {
				runtime.GC()
				runtime.Gosched()
			}
		})
	}

	kept := make([]Gache[int], retainedInstances)

	for i := range numInstances {
		gc := New[int]()
		// Populate keys across diverse shards
		for k := range keysPerInstance {
			key := fmt.Sprintf("gc_test_key_%d_%d", i, k)
			gc.Set(key, i*1000+k)
		}

		if i < retainedInstances {
			kept[i] = gc
		}

		if i%500 == 0 {
			runtime.Gosched()
		}
	}

	// Stress-test retained instances concurrently while GC is furiously executing
	const workerCount = 16
	var opWg sync.WaitGroup
	opWg.Add(workerCount)

	for w := range workerCount {
		go func(workerID int) {
			defer opWg.Done()
			r := rand.New(rand.NewSource(int64(workerID * 999)))
			for end := time.Now().Add(stressBudget(300 * time.Millisecond)); time.Now().Before(end); {
				idx := r.Intn(retainedInstances)
				gc := kept[idx]
				k := r.Intn(keysPerInstance)
				key := fmt.Sprintf("gc_test_key_%d_%d", idx, k)

				// Verify Get
				val, ok := gc.Get(key)
				if ok {
					expected := idx*1000 + k
					if val != expected && val != expected+50000 {
						t.Errorf("Worker %d: data corruption in instance %d key %s: got %d, expected %d",
							workerID, idx, key, val, expected)
					}

				}

				// Mutate entry
				if r.Float32() < 0.2 {
					gc.Set(key, idx*1000+k+50000)
				}

				// Verify interior shard arena pointers
				gInternal, ok := gc.(*gache[int])
				if ok {

					// Spot-check random shard
					sIdx := r.Intn(slen)
					shardPtr := gInternal.shards[sIdx]
					if shardPtr == nil {
						t.Errorf("Worker %d: shard pointer %d was collected or is nil", workerID, sIdx)
						return
					}
					if n := shardPtr.length(); n < 0 {
						t.Errorf("Worker %d: shard %d has negative length %d", workerID, sIdx, n)
					}
				}
			}
		}(w)
	}

	opWg.Wait()
	stopGC.Store(true)
	gcWg.Wait()

	// Final integrity sweep of all retained instances
	for idx, gc := range kept {
		if gc.Len() <= 0 {
			t.Errorf("Instance %d Len() unexpectedly empty: %d", idx, gc.Len())
		}
		gInternal := gc.(*gache[int])
		for s := range slen {
			if gInternal.shards[s] == nil {
				t.Fatalf("Instance %d shard %d pointer is nil after GC stress", idx, s)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Shard-level checks (need the shard arena)
// ---------------------------------------------------------------------------

// oldKeyShard returns a cache holding "old_key" = 42 and the shard that owns it,
// after three lookups of absent keys on that shard (the lookups of absent keys
// must leave the stored entry alone).
func oldKeyShard(t *testing.T) (Gache[int], *shardMap[string, int], uint64) {
	t.Helper()
	gc := New[int]()
	gc.Set("old_key", 42)
	kl := gc.(*gache[int]).maxKeyLength
	shard := gc.(*gache[int]).shards[getShardID("old_key", kl)&mask]
	for _, k := range []string{"nonexistent1", "nonexistent2", "nonexistent3"} {
		shard.loadNode(getShardID(k, kl), k)
	}
	return gc, shard, kl
}

// TestGache_AbsentLookupsKeepExistingKey: lookups of absent keys on a shard, a
// reserve of the shard and a second key landing on the same shard never lose the
// entry stored before.
func TestGache_AbsentLookupsKeepExistingKey(t *testing.T) {
	gc, shard, kl := oldKeyShard(t)
	shard.initReserve(10)

	for i := range 10000 {
		key := fmt.Sprintf("k_%d", i)
		if getShardID(key, kl)&mask == getShardID("old_key", kl)&mask {
			gc.Set(key, 99)
			break
		}
	}

	for i := range 100 {
		k := fmt.Sprintf("miss_%d", i)
		shard.loadNode(getShardID(k, kl), k)
	}

	if val, ok := gc.Get("old_key"); !ok || val != 42 {
		t.Fatalf("old_key lost: ok=%v, val=%v, want 42", ok, val)
	}
}

// countShards counts the entries the shards of g hold by ranging over each shard,
// the oracle Len is compared with.
func countShards[V any](g *gache[V]) (n int) {
	for i := range g.shards {
		g.shards[i].rangeShard(func(string, *value[string, V]) bool {
			n++
			return true
		})
	}
	return n
}

// TestGache_LenClearConcurrent: Clear racing writers and deleters leaves Len equal to the number of entries the shards hold.
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

	if got, actual := g.Len(), countShards(g); got != actual {
		t.Fatalf("final Len() = %d, counted %d entries", got, actual)
	}
}

// TestGache_LenConcurrent: after a fixed number of concurrent Set/Delete operations per goroutine, Len equals the number of entries the shards hold.
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

	if got, actual := g.Len(), countShards(g); got != actual {
		t.Fatalf("after concurrent ops, Len() = %d, but counted %d entries", got, actual)
	}
}

// TestGache_ReadDoesNotDropExisting: Read of a gob stream with many keys keeps an entry stored before it.
func TestGache_ReadDoesNotDropExisting(t *testing.T) {
	gc, shard, kl := oldKeyShard(t)
	gc.Get("old_key1")

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
		k := fmt.Sprintf("miss_%d", i)
		shard.loadNode(getShardID(k, kl), k)
	}

	if val, ok := gc.Get("old_key"); !ok || val != 42 {
		t.Fatalf("old_key lost: ok=%v, val=%v, want 42", ok, val)
	}
}

// BenchmarkGache_Loop measures the execution speed of internal iteration over the cache's shard segments and values.
func BenchmarkGache_Loop(b *testing.B) {
	sizes := []int{10_000, 100_000, 1_000_000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			ctx := context.Background()
			g := New[string]().(*gache[string])
			for i := range size {
				g.Set(fmt.Sprintf("key-%d", i), "value")
			}

			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				g.loop(ctx, func(k string, v *value[string, string]) bool {
					return true
				})
			}
		})
	}
}
