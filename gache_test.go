package gache_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gache "github.com/kpango/gache/v2"
)

// ---------------------------------------------------------------------------
// Contract matrix: every method listed in contractOps x value kind x entry state.
// ---------------------------------------------------------------------------

type contractPayload struct{ a, b uint64 }

// entryState is one way an entry can be in the cache. setup leaves the key k in
// that state using a and b (two distinct values); the rest is the expectation.
type entryState[V comparable] struct {
	setup func(t *testing.T, g gache.Gache[V], k string, a, b V)
	name  string
	live  bool // Get sees the entry
	stale bool // expired but still stored: GetWithIgnoredExpire sees it
	ttl   bool // the live entry expires
	useB  bool // the live value is b, not a
}

func entryStates[V comparable]() []entryState[V] {
	expire := func(t *testing.T, g gache.Gache[V], k string, a V) {
		t.Helper()
		g.SetWithExpire(k, a, time.Millisecond)
		gache.WaitCacheElapsed(t, time.Millisecond)
	}
	return []entryState[V]{
		{name: "absent", setup: func(*testing.T, gache.Gache[V], string, V, V) {}},
		{name: "live without TTL", live: true, setup: func(_ *testing.T, g gache.Gache[V], k string, a, _ V) {
			g.SetWithExpire(k, a, gache.NoTTL)
		}},
		{name: "live with TTL", live: true, ttl: true, setup: func(_ *testing.T, g gache.Gache[V], k string, a, _ V) {
			g.SetWithExpire(k, a, time.Hour)
		}},
		{name: "expired", stale: true, setup: func(t *testing.T, g gache.Gache[V], k string, a, _ V) { expire(t, g, k, a) }},
		{name: "deleted", setup: func(_ *testing.T, g gache.Gache[V], k string, a, _ V) {
			g.Set(k, a)
			g.Delete(k)
		}},
		{name: "re-set after delete", live: true, useB: true, setup: func(_ *testing.T, g gache.Gache[V], k string, a, b V) {
			g.SetWithExpire(k, a, time.Hour)
			g.Delete(k)
			g.SetWithExpire(k, b, gache.NoTTL)
		}},
		{name: "overwritten", live: true, ttl: true, useB: true, setup: func(_ *testing.T, g gache.Gache[V], k string, a, b V) {
			g.SetWithExpire(k, a, gache.NoTTL)
			g.Set(k, b)
		}},
		{name: "re-set after expiry", live: true, ttl: true, useB: true, setup: func(t *testing.T, g gache.Gache[V], k string, a, b V) {
			expire(t, g, k, a)
			g.Set(k, b)
		}},
	}
}

// cell is one (value kind, entry state) pair with a cache in that state.
type cell[V comparable] struct {
	g  gache.Gache[V]
	a  V
	b  V
	t  *testing.T
	k  string
	st entryState[V]
}

// want is the value Get must report and whether it must report one.
func (c *cell[V]) want() (v V, ok bool) {
	if !c.st.live {
		return v, false
	}
	if c.st.useB {
		return c.b, true
	}
	return c.a, true
}

func (c *cell[V]) wantLen() int {
	if c.st.live {
		return 1
	}
	return 0
}

func (c *cell[V]) mustGet(what string, wantV V, wantOK bool) {
	c.t.Helper()
	if v, ok := c.g.Get(c.k); ok != wantOK || (ok && v != wantV) {
		c.t.Fatalf("%s: Get = (%v, %v), want (%v, %v)", what, v, ok, wantV, wantOK)
	}
}

func (c *cell[V]) expire(what string) int64 {
	c.t.Helper()
	_, e, ok := c.g.GetWithExpire(c.k)
	if !ok {
		c.t.Fatalf("%s: the entry is gone", what)
	}
	return e
}

// mustExpireAfter requires the entry to expire more than d from now.
func (c *cell[V]) mustExpireAfter(what string, d time.Duration) {
	c.t.Helper()
	if e := c.expire(what); e <= gache.CacheNow()+int64(d) {
		c.t.Fatalf("%s: expire %v from now, want more than %v", what, time.Duration(e-gache.CacheNow()), d)
	}
}

func (c *cell[V]) mustNotExpire(what string) {
	c.t.Helper()
	if e := c.expire(what); e > 0 {
		c.t.Fatalf("%s: expire = %d, want an entry without TTL", what, e)
	}
}

// checkSnapshots asserts what Range, Keys, Values, ToRawMap and ToMap report for the cell.
func (c *cell[V]) checkSnapshots() {
	c.t.Helper()
	ctx := context.Background()
	wantV, live := c.want()

	var mu sync.Mutex // Range calls f from several goroutines
	ranged := map[string]V{}
	c.g.Range(ctx, func(k string, v V, e int64) bool {
		mu.Lock()
		ranged[k] = v
		mu.Unlock()
		if (e > 0) != c.st.ttl {
			c.t.Errorf("Range passed expire %d, entry has TTL = %v", e, c.st.ttl)
		}
		return true
	})
	keys, vals, raw := c.g.Keys(ctx), c.g.Values(ctx), c.g.ToRawMap(ctx)
	tm := map[string]V{}
	c.g.ToMap(ctx).Range(func(k, v any) bool { tm[k.(string)] = v.(V); return true })

	if keys == nil || vals == nil || raw == nil {
		c.t.Fatalf("Keys/Values/ToRawMap = (%v, %v, %v), want non-nil results", keys == nil, vals == nil, raw == nil)
	}
	n := c.wantLen()
	if len(ranged) != n || len(keys) != n || len(vals) != n || len(raw) != n || len(tm) != n {
		c.t.Fatalf("Range=%d Keys=%d Values=%d ToRawMap=%d ToMap=%d entries, want %d each", len(ranged), len(keys), len(vals), len(raw), len(tm), n)
	}
	if live {
		if keys[0] != c.k || vals[0] != wantV || raw[c.k] != wantV || tm[c.k] != wantV || ranged[c.k] != wantV {
			c.t.Fatalf("snapshots = keys %v vals %v raw %v ToMap %v Range %v, want %q -> %v", keys, vals, raw, tm, ranged, c.k, wantV)
		}
	}
}

// cellOp is one Gache[V] method (or method variant) applied to a cell, with its
// expectations for every entry state.
type cellOp[V comparable] struct {
	run  func(c *cell[V])
	name string
}

func contractOps[V comparable]() []cellOp[V] {
	getRefreshWithDur := func(name string, d time.Duration, checkTTL func(c *cell[V])) cellOp[V] {
		return cellOp[V]{name: name, run: func(c *cell[V]) {
			wantV, wantOK := c.want()
			if v, ok := c.g.GetRefreshWithDur(c.k, d); ok != wantOK || (ok && v != wantV) {
				c.t.Fatalf("GetRefreshWithDur = (%v, %v), want (%v, %v)", v, ok, wantV, wantOK)
			}
			if wantOK {
				checkTTL(c)
			}
		}}
	}
	// setIfNotExists: a live entry keeps its value and expire; otherwise b is stored
	// with the TTL that checkTTL asserts.
	setIfNotExists := func(name string, set func(c *cell[V]), checkTTL func(c *cell[V])) cellOp[V] {
		return cellOp[V]{name: name, run: func(c *cell[V]) {
			var before int64
			if c.st.live {
				before = c.expire("before")
			}
			set(c)
			if wantV, ok := c.want(); ok {
				c.mustGet(name+" on a live entry", wantV, true)
				if e := c.expire("after"); e != before {
					c.t.Fatalf("%s changed the expire of a live entry: %d -> %d", name, before, e)
				}
				return
			}
			c.mustGet(name, c.b, true)
			checkTTL(c)
		}}
	}
	return []cellOp[V]{
		{name: "Get", run: func(c *cell[V]) {
			v, ok := c.want()
			c.mustGet("Get", v, ok)
			// Get evicts an expired entry, so nothing stays behind.
			if n := c.g.Len(); n != c.wantLen() {
				c.t.Fatalf("Len after Get = %d, want %d", n, c.wantLen())
			}
			if _, ok := c.g.GetWithIgnoredExpire(c.k); ok != c.st.live {
				c.t.Fatalf("GetWithIgnoredExpire after Get = %v, want %v (Get evicts an expired entry)", ok, c.st.live)
			}
		}},
		{name: "GetWithExpire", run: func(c *cell[V]) {
			wantV, wantOK := c.want()
			v, e, ok := c.g.GetWithExpire(c.k)
			if ok != wantOK || (ok && (v != wantV || (e > 0) != c.st.ttl)) {
				c.t.Fatalf("GetWithExpire = (%v, %d, %v), want (%v, TTL=%v, %v)", v, e, ok, wantV, c.st.ttl, wantOK)
			}
		}},
		{name: "GetWithIgnoredExpire", run: func(c *cell[V]) {
			v, ok := c.g.GetWithIgnoredExpire(c.k)
			wantV, _ := c.want()
			if c.st.stale {
				wantV = c.a
			}
			if wantOK := c.st.live || c.st.stale; ok != wantOK || (ok && v != wantV) {
				c.t.Fatalf("GetWithIgnoredExpire = (%v, %v), want (%v, %v)", v, ok, wantV, wantOK)
			}
		}},
		{name: "GetRefresh", run: func(c *cell[V]) {
			wantV, wantOK := c.want()
			if v, ok := c.g.GetRefresh(c.k); ok != wantOK || (ok && v != wantV) {
				c.t.Fatalf("GetRefresh = (%v, %v), want (%v, %v)", v, ok, wantV, wantOK)
			}
			if wantOK {
				c.mustExpireAfter("GetRefresh (default TTL 30s)", 20*time.Second)
			} else {
				c.mustGet("after GetRefresh", wantV, false)
			}
		}},
		getRefreshWithDur("GetRefreshWithDur NoTTL", gache.NoTTL, func(c *cell[V]) { c.mustNotExpire("GetRefreshWithDur(NoTTL)") }),
		getRefreshWithDur("GetRefreshWithDur 2h", 2*time.Hour, func(c *cell[V]) { c.mustExpireAfter("GetRefreshWithDur(2h)", 90*time.Minute) }),
		{name: "ExtendExpire", run: func(c *cell[V]) {
			var before int64
			if c.st.live {
				before = c.expire("before")
			}
			c.g.ExtendExpire(c.k, time.Hour)
			wantV, wantOK := c.want()
			c.mustGet("after ExtendExpire", wantV, wantOK)
			switch {
			case wantOK && c.st.ttl:
				if e := c.expire("ExtendExpire"); e != before+int64(time.Hour) {
					c.t.Fatalf("expire moved by %v, want exactly 1h", time.Duration(e-before))
				}
			case wantOK:
				c.mustNotExpire("ExtendExpire on an entry without TTL")
			}
		}},
		{name: "Pop", run: func(c *cell[V]) {
			wantV, wantOK := c.want()
			if v, ok := c.g.Pop(c.k); ok != wantOK || (ok && v != wantV) {
				c.t.Fatalf("Pop = (%v, %v), want (%v, %v)", v, ok, wantV, wantOK)
			}
			c.mustGet("after Pop", wantV, false)
			if n := c.g.Len(); n != 0 {
				c.t.Fatalf("Len after Pop = %d, want 0", n)
			}
		}},
		{name: "Delete", run: func(c *cell[V]) {
			wantV, wantOK := c.want()
			v, loaded := c.g.Delete(c.k)
			if (c.st.live && (!loaded || v != wantV)) || (!c.st.live && !c.st.stale && loaded) {
				c.t.Fatalf("Delete = (%v, %v), want (%v, %v)", v, loaded, wantV, wantOK)
			}
			c.mustGet("after Delete", wantV, false)
			if _, ok := c.g.GetWithIgnoredExpire(c.k); ok {
				c.t.Fatal("GetWithIgnoredExpire still sees the entry after Delete")
			}
			if n := c.g.Len(); n != 0 {
				c.t.Fatalf("Len after Delete = %d, want 0", n)
			}
		}},
		{name: "Set", run: func(c *cell[V]) {
			c.g.Set(c.k, c.b)
			c.mustGet("Set", c.b, true)
			c.mustExpireAfter("Set (default TTL 30s)", 20*time.Second)
			if n := c.g.Len(); n != 1 {
				c.t.Fatalf("Len after Set = %d, want 1", n)
			}
		}},
		{name: "SetWithExpire NoTTL", run: func(c *cell[V]) {
			c.g.SetWithExpire(c.k, c.b, gache.NoTTL)
			c.mustGet("SetWithExpire", c.b, true)
			c.mustNotExpire("SetWithExpire(NoTTL)")
		}},
		// The same value again with a new TTL: it must refresh the expiry of a live
		// entry and revive an expired one (the lock-free same-value path of store).
		{name: "SetWithExpire zero and negative TTL", run: func(c *cell[V]) {
			for _, d := range []time.Duration{0, -10 * time.Minute} {
				c.g.SetWithExpire(c.k, c.b, d)
				c.mustGet("SetWithExpire", c.b, true)
				c.mustNotExpire(fmt.Sprintf("SetWithExpire(%v)", d))
			}
		}},
		{name: "SetWithExpire same value", run: func(c *cell[V]) {
			c.g.SetWithExpire(c.k, c.a, 2*time.Hour)
			c.mustGet("SetWithExpire(a)", c.a, true)
			c.mustExpireAfter("SetWithExpire(a, 2h)", 90*time.Minute)
		}},
		{name: "SetWithExpire same value NoTTL", run: func(c *cell[V]) {
			c.g.SetWithExpire(c.k, c.a, gache.NoTTL)
			c.mustGet("SetWithExpire(a)", c.a, true)
			c.mustNotExpire("SetWithExpire(a, NoTTL)")
		}},
		setIfNotExists("SetIfNotExists",
			func(c *cell[V]) { c.g.SetIfNotExists(c.k, c.b) },
			func(c *cell[V]) { c.mustExpireAfter("SetIfNotExists (default TTL 30s)", 20*time.Second) }),
		setIfNotExists("SetWithExpireIfNotExists",
			func(c *cell[V]) { c.g.SetWithExpireIfNotExists(c.k, c.b, 2*time.Hour) },
			func(c *cell[V]) { c.mustExpireAfter("SetWithExpireIfNotExists(2h)", 90*time.Minute) }),
		{name: "Range Keys Values ToRawMap ToMap", run: func(c *cell[V]) {
			c.checkSnapshots()
			if n := c.g.Len(); n != c.wantLen() && !c.st.stale {
				c.t.Fatalf("Len = %d, want %d", n, c.wantLen())
			}
		}},
		{name: "DeleteExpired", run: func(c *cell[V]) {
			want := uint64(0)
			if c.st.stale {
				want = 1
			}
			if n := c.g.DeleteExpired(context.Background()); n != want {
				c.t.Fatalf("DeleteExpired = %d, want %d", n, want)
			}
			if n := c.g.Len(); n != c.wantLen() {
				c.t.Fatalf("Len after DeleteExpired = %d, want %d", n, c.wantLen())
			}
			wantV, wantOK := c.want()
			c.mustGet("after DeleteExpired", wantV, wantOK)
		}},
		{name: "Clear", run: func(c *cell[V]) {
			c.g.Clear()
			c.mustGet("after Clear", c.a, false)
			if _, ok := c.g.GetWithIgnoredExpire(c.k); ok || c.g.Len() != 0 {
				c.t.Fatalf("Clear left an entry behind (Len = %d)", c.g.Len())
			}
		}},
	}
}

func runContract[V comparable](t *testing.T, a, b V) {
	t.Helper()
	for _, st := range entryStates[V]() {
		t.Run(st.name, func(t *testing.T) {
			t.Parallel()

			for _, op := range contractOps[V]() {
				t.Run(op.name, func(t *testing.T) {
					t.Parallel()

					c := &cell[V]{t: t, g: gache.New[V](), st: st, k: "key", a: a, b: b}
					defer c.g.Stop()
					st.setup(t, c.g, c.k, a, b)
					op.run(c)
				})
			}
		})
	}
}

// Every method in contractOps behaves the same for a word-sized value, a string, a
// multi-word value and a pointer, in every entry state of entryStates. An expired
// entry is absent for Get, GetWithExpire, GetRefresh, Range and the snapshots (only
// GetWithIgnoredExpire still sees it) and nothing revives it but a write.
func TestGache_Contract(t *testing.T) {
	t.Parallel()

	t.Run("int", func(t *testing.T) { t.Parallel(); runContract(t, 1, 2) })
	t.Run("string", func(t *testing.T) { t.Parallel(); runContract(t, "first value", "second value") })
	t.Run("[4]int64", func(t *testing.T) {
		t.Parallel()
		runContract(t, [4]int64{1, 2, 3, 4}, [4]int64{5, 6, 7, 8})
	})
	t.Run("pointer", func(t *testing.T) {
		t.Parallel()
		runContract(t, &contractPayload{a: 1, b: ^uint64(1)}, &contractPayload{a: 2, b: ^uint64(2)})
	})
}

// Keys of every length class (empty, one byte, hashed, long, multi-byte runes) are
// stored, read, listed and removed exactly, with and without a key length limit.
func TestGache_KeyKinds(t *testing.T) {
	t.Parallel()

	keys := []string{
		"", "a", "z", "\x00", "\xff", " ", "ab", strings.Repeat("k", 32), strings.Repeat("k", 33),
		strings.Repeat("k", 64), strings.Repeat("k", 65), strings.Repeat("k", 300),
		"こんにちは", "🚀", "👨‍👩‍👧‍👦", "مرحبا", "café_au_lait",
	}
	for _, kl := range []uint64{0, 1, 16, 256} {
		t.Run(fmt.Sprintf("maxKeyLength=%d", kl), func(t *testing.T) {
			t.Parallel()

			g := gache.New[int](gache.WithMaxKeyLength[int](kl))
			defer g.Stop()
			for i, k := range keys {
				g.Set(k, i)
			}
			if n := g.Len(); n != len(keys) {
				t.Fatalf("Len = %d, want %d: distinct keys must stay distinct entries", n, len(keys))
			}
			raw := g.ToRawMap(t.Context())
			for i, k := range keys {
				if v, ok := g.Get(k); !ok || v != i {
					t.Fatalf("Get(%q) = (%d, %v), want (%d, true)", k, v, ok, i)
				}
				if raw[k] != i {
					t.Fatalf("ToRawMap[%q] = %d, want %d", k, raw[k], i)
				}
			}
			for i, k := range keys {
				if v, ok := g.Pop(k); !ok || v != i {
					t.Fatalf("Pop(%q) = (%d, %v), want (%d, true)", k, v, ok, i)
				}
			}
			if n := g.Len(); n != 0 {
				t.Fatalf("Len after popping every key = %d, want 0", n)
			}
		})
	}
}

// Keys, Values, ToRawMap, ToMap, Range, Size and Write/Read over many keys, which
// spread across the shards, report exactly what was stored.
func TestGache_Collective(t *testing.T) {
	t.Parallel()

	const n = 300
	g := gache.New[int]()
	defer g.Stop()
	empty := g.Size()
	want := make(map[string]int, n)
	for i := range n {
		k := fmt.Sprintf("shard_key_%03d", i)
		want[k] = i
		g.Set(k, i)
	}
	if g.Len() != n || g.Size() <= empty {
		t.Fatalf("Len = %d (want %d), Size = %d (empty %d, want it to grow)", g.Len(), n, g.Size(), empty)
	}

	same := func(what string, got map[string]int) {
		t.Helper()
		if len(got) != n {
			t.Fatalf("%s: %d entries, want %d", what, len(got), n)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s: %q = %d, want %d", what, k, got[k], v)
			}
		}
	}
	ctx := t.Context()
	same("ToRawMap", g.ToRawMap(ctx))

	keys, vals := g.Keys(ctx), g.Values(ctx)
	fromKeys, sum := map[string]int{}, 0
	for _, k := range keys {
		fromKeys[k] = want[k]
	}
	same("Keys", fromKeys)
	for _, v := range vals {
		sum += v
	}
	if len(vals) != n || sum != n*(n-1)/2 {
		t.Fatalf("Values = %d values summing to %d, want %d summing to %d", len(vals), sum, n, n*(n-1)/2)
	}

	var mu sync.Mutex // Range calls f from several goroutines
	ranged := map[string]int{}
	g.Range(ctx, func(k string, v int, _ int64) bool { mu.Lock(); ranged[k] = v; mu.Unlock(); return true })
	same("Range", ranged)
	tm := map[string]int{}
	g.ToMap(ctx).Range(func(k, v any) bool { tm[k.(string)] = v.(int); return true })
	same("ToMap", tm)

	var buf bytes.Buffer
	if err := g.Write(ctx, &buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	restored := gache.New[int]()
	defer restored.Stop()
	if err := restored.Read(&buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	same("Write then Read", restored.ToRawMap(ctx))
}

// Len follows every mutation, one step after the other.
func TestGache_LenSteps(t *testing.T) {
	t.Parallel()

	g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))
	defer g.Stop()
	steps := []struct {
		do   func()
		name string
		want int
	}{
		{func() {}, "empty", 0},
		{func() { g.Set("a", 1); g.Set("b", 2); g.Set("c", 3) }, "three Sets", 3},
		{func() { g.Set("b", 20) }, "overwrite", 3},
		{func() { g.Delete("a") }, "Delete", 2},
		{func() { g.Delete("nonexistent") }, "Delete of an absent key", 2},
		{func() { g.Pop("b") }, "Pop", 1},
		{func() { g.SetIfNotExists("d", 4) }, "SetIfNotExists on a new key", 2},
		{func() { g.SetIfNotExists("d", 40) }, "SetIfNotExists on an existing key", 2},
		{func() { g.Clear() }, "Clear", 0},
	}
	for _, s := range steps {
		s.do()
		if got := g.Len(); got != s.want {
			t.Fatalf("after %s, Len() = %d, want %d", s.name, got, s.want)
		}
	}
}

// The hook fires, once and with the entry's key and value, for every call that
// evicts an expired entry, and not at all while it is disabled.
func TestGache_ExpiredHookOnEviction(t *testing.T) {
	t.Parallel()

	evictors := []struct {
		do   func(g gache.Gache[string], k string)
		name string
	}{
		{func(g gache.Gache[string], k string) { g.Get(k) }, "Get"},
		{func(g gache.Gache[string], k string) { g.GetWithExpire(k) }, "GetWithExpire"},
		{func(g gache.Gache[string], k string) { g.GetRefresh(k) }, "GetRefresh"},
		{func(g gache.Gache[string], k string) { g.GetRefreshWithDur(k, time.Hour) }, "GetRefreshWithDur"},
		{func(g gache.Gache[string], k string) { g.ExtendExpire(k, time.Hour) }, "ExtendExpire"},
		{func(g gache.Gache[string], k string) { g.Pop(k) }, "Pop"},
		{func(g gache.Gache[string], _ string) {
			g.Range(context.Background(), func(string, string, int64) bool { return true })
		}, "Range"},
		{func(g gache.Gache[string], _ string) { g.ToMap(context.Background()) }, "ToMap"},
		{func(g gache.Gache[string], _ string) { g.DeleteExpired(context.Background()) }, "DeleteExpired"},
	}
	for _, e := range evictors {
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu   sync.Mutex
				seen = map[string]string{}
				n    = map[string]int{}
			)
			g := gache.New[string]().
				SetExpiredHook(func(_ context.Context, k, v string) {
					mu.Lock()
					seen[k], n[k] = v, n[k]+1
					mu.Unlock()
				}).
				EnableExpiredHook().
				StartExpired(t.Context(), time.Hour) // only drains the queue: no periodic sweep
			defer g.Stop()

			g.SetWithExpire("while-disabled", "v1", time.Millisecond)
			gache.WaitCacheElapsed(t, time.Millisecond)
			g.DisableExpiredHook()
			e.do(g, "while-disabled")
			g.EnableExpiredHook()

			g.SetWithExpire("while-enabled", "v2", time.Millisecond)
			gache.WaitCacheElapsed(t, time.Millisecond)
			e.do(g, "while-enabled")

			gache.WaitUntil(t, "the expired hook to fire", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return n["while-enabled"] > 0
			})
			mu.Lock()
			defer mu.Unlock()
			if n["while-enabled"] != 1 || seen["while-enabled"] != "v2" {
				t.Errorf("hook ran %d times for the enabled key with value %q, want once with \"v2\"", n["while-enabled"], seen["while-enabled"])
			}
			if n["while-disabled"] != 0 {
				t.Errorf("hook ran %d times for the key evicted while it was disabled, want 0", n["while-disabled"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrency regressions and stress (public API)
// ---------------------------------------------------------------------------

// TestGache_DataRace runs a fixed number of concurrent mixed operations (100
// goroutines x 1000 ops); it finds data races when run with -race.
func TestGache_DataRace(t *testing.T) {
	c := gache.New[string]()
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

// TestGache_LenConcurrentStoreDelete tests the integrity of the item counter when multiple goroutines symmetrically store and delete items.
func TestGache_LenConcurrentStoreDelete(t *testing.T) {
	t.Parallel()
	g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))

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

// TestGache_ContextCancellation verifies that Range stops when its context is cancelled.
func TestGache_ContextCancellation(t *testing.T) {
	g := gache.New[int]()
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

// ---------------------------------------------------------------------------
// Revive races and same-key churn (public API)
// ---------------------------------------------------------------------------

// reviveNeverExposesWrongValue pins the ABA bug: a lock-free Get racing a Set that
// revives a tombstoned node reported the key present with a value other than the
// one Set stored (the zero value), because the reviver made the node live before
// publishing the payload.
func reviveNeverExposesWrongValue[V comparable](t *testing.T, val V) {
	t.Helper()
	g := gache.New[V]()
	hits, bad := gache.RunReviveChurn(
		func() { g.Set("k", val) },
		func() { g.Delete("k") },
		func() (bool, bool) { v, ok := g.Get("k"); return ok, v == val },
	)
	if bad != 0 {
		t.Fatalf("%d of %d hits returned a value other than the stored one", bad, hits)
	}
	if hits == 0 {
		t.Fatal("readers never observed the key; the race was not exercised")
	}
}

func TestGache_ReviveNeverExposesZeroValue(t *testing.T) {
	t.Parallel()

	type payload struct{ a, b uint64 }
	pv := &payload{a: 7, b: ^uint64(7)}
	t.Run("int", func(t *testing.T) { t.Parallel(); reviveNeverExposesWrongValue(t, 5) })
	t.Run("string", func(t *testing.T) {
		t.Parallel()
		reviveNeverExposesWrongValue(t, "a-value-that-is-not-the-zero-value-of-string")
	})
	t.Run("pointer", func(t *testing.T) { t.Parallel(); reviveNeverExposesWrongValue(t, pv) })
}

// Regression: a revive racing a delete of the very node it revives drifted Len
// away from what Range visits (no TTL involved, dirty-only churn on few and many shards).
func TestGache_SameKeyChurnLenMatchesRange(t *testing.T) {
	t.Parallel()

	const keys = 4
	for _, churnKeys := range []int{16, 4096} {
		t.Run(fmt.Sprintf("churnKeys=%d", churnKeys), func(t *testing.T) {
			t.Parallel()

			gache.SameKeyRounds(func(n int) {
				g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))
				for k := range keys {
					g.Set(fmt.Sprint(k), k)
				}
				gache.SameKeyChurn(churnKeys,
					func(k, v int) { g.Set(fmt.Sprint(k), v) },
					func(k, _ int) { g.Delete(fmt.Sprint(k)) },
					func(k, v int) { g.Set(fmt.Sprint(k), v) },
					func(k, _ int) { g.Delete(fmt.Sprint(k)) },
				)

				var gr atomic.Int64 // Range calls f from several goroutines
				g.Range(context.Background(), func(string, int, int64) bool { gr.Add(1); return true })
				if g.Len() != int(gr.Load()) {
					t.Fatalf("round %d: Gache.Len = %d, Gache.Range visited %d", n, g.Len(), gr.Load())
				}
			})
		})
	}
}

// Regression: a pointer-shaped V stored with a plain uint64 store is invisible to
// the GC write barrier, so its pointee was freed while the cache still held it.
func TestGache_PointerValuesSurviveConcurrentGC(t *testing.T) {
	t.Parallel()

	g := gache.New[*gache.GCPayload]()
	gache.GCStress(t,
		func(k int, p *gache.GCPayload) { g.Set(fmt.Sprint(k), p) },
		func(k int) { g.Delete(fmt.Sprint(k)) },
		func(k int) *gache.GCPayload { p, _ := g.Get(fmt.Sprint(k)); return p },
	)
}

// Keys longer than MaxKeyLength with one shared prefix all hash alike: store, get,
// delete and update stay exact for each of them.
func TestGache_MaxKeyLengthPrefixCollision(t *testing.T) {
	maxKeyLen := uint64(8)
	g := gache.New[string](gache.WithMaxKeyLength[string](maxKeyLen))
	defer g.Stop()

	const numKeys = 200
	keys := make([]string, numKeys)
	prefix := "common_prefix_"
	for i := range numKeys {
		keys[i] = fmt.Sprintf("%s_suffix_%04d_payload_padding", prefix, i)
	}

	// 1. Store all keys
	for i, k := range keys {
		g.Set(k, fmt.Sprintf("val_%d", i))
	}

	// Verify all keys are retrievable
	for i, k := range keys {
		val, ok := g.Get(k)
		if !ok || val != fmt.Sprintf("val_%d", i) {
			t.Fatalf("failed to retrieve key %s: got (%q, %v)", k, val, ok)
		}
	}

	// Verify Len() is exactly numKeys
	if l := g.Len(); l != numKeys {
		t.Fatalf("Len() = %d, want %d", l, numKeys)
	}

	// 2. Delete even-indexed keys
	for i := 0; i < numKeys; i += 2 {
		deleted, ok := g.Delete(keys[i])
		if !ok || deleted != fmt.Sprintf("val_%d", i) {
			t.Fatalf("failed to delete key %s: got (%q, %v)", keys[i], deleted, ok)
		}
	}

	// Verify odd-indexed keys still exist, even-indexed do not
	for i := range numKeys {
		val, ok := g.Get(keys[i])
		if i%2 == 0 {
			if ok {
				t.Fatalf("deleted key %s still present: got %q", keys[i], val)
			}
		} else {
			if !ok || val != fmt.Sprintf("val_%d", i) {
				t.Fatalf("live key %s corrupted or missing: got (%q, %v)", keys[i], val, ok)
			}
		}
	}

	// 3. Update existing odd-indexed keys
	for i := 1; i < numKeys; i += 2 {
		g.Set(keys[i], fmt.Sprintf("updated_%d", i))
	}

	// Trigger promotions by reading / adding keys
	for i := range 50 {
		g.Set(fmt.Sprintf("other_key_%d", i), "other")
	}

	for i := 1; i < numKeys; i += 2 {
		val, ok := g.Get(keys[i])
		if !ok || val != fmt.Sprintf("updated_%d", i) {
			t.Fatalf("updated live key %s wrong value: got (%q, %v), want %q", keys[i], val, ok, fmt.Sprintf("updated_%d", i))
		}
	}
}

// Len, Range and Get match an exact oracle of live keys: 120 goroutines operate on
// disjoint key partitions (all colliding on the 8-byte hash) without Clear.
func TestGache_LenMatchesDisjointOracle(t *testing.T) {
	const numGoroutines = 120
	const keysPerGoroutine = 5
	const iterations = 300

	// Use MaxKeyLength=8 to create dense collision clusters within shards
	g := gache.New[int](gache.WithMaxKeyLength[int](8))
	defer g.Stop()

	type workerOracle struct {
		live map[string]int
		mu   sync.Mutex
	}

	oracles := make([]workerOracle, numGoroutines)
	for i := range oracles {
		oracles[i].live = make(map[string]int)
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for id := range numGoroutines {
		go func(workerID int) {
			defer wg.Done()
			for iter := range iterations {
				k := fmt.Sprintf("prefix__w%03d_k%d", workerID, iter%keysPerGoroutine)
				val := workerID*1000 + iter
				op := (workerID + iter) % 4

				oracles[workerID].mu.Lock()
				switch op {
				case 0, 1: // Set (insert or update)
					g.Set(k, val)
					oracles[workerID].live[k] = val
				case 2: // Delete
					g.Delete(k)
					delete(oracles[workerID].live, k)
				case 3: // Pop
					g.Pop(k)
					delete(oracles[workerID].live, k)
				}
				oracles[workerID].mu.Unlock()
			}
		}(id)
	}

	wg.Wait()

	// Aggregate global oracle
	totalExpectedLive := 0
	expectedKeys := make(map[string]int)
	for i := range oracles {
		for k, v := range oracles[i].live {
			expectedKeys[k] = v
			totalExpectedLive++
		}
	}

	// Verification: Len() must match oracle exactly
	actualLen := g.Len()
	if actualLen != totalExpectedLive {
		t.Fatalf("Len() mismatch against Oracle! Len()=%d, Oracle=%d",
			actualLen, totalExpectedLive)
	}

	// Verify Range matches oracle
	// Range calls the callback from several goroutines: count atomically and report
	// with Errorf (Fatalf is only allowed on the test goroutine).
	var countedRange atomic.Int64
	g.Range(context.Background(), func(k string, v int, exp int64) bool {
		countedRange.Add(1)
		expectedVal, exists := expectedKeys[k]
		if !exists {
			t.Errorf("Range found key %q not in oracle", k)
		} else if v != expectedVal {
			t.Errorf("Range key %q value mismatch: got %d, want %d", k, v, expectedVal)
		}
		return true
	})

	if got := countedRange.Load(); got != int64(totalExpectedLive) {
		t.Fatalf("Range count %d does not match Oracle %d", got, totalExpectedLive)
	}

	// Verify all oracle keys are retrievable via Get()
	for k, expectedVal := range expectedKeys {
		v, ok := g.Get(k)
		if !ok {
			t.Fatalf("Oracle live key %q not found by Get()!", k)
		}
		if v != expectedVal {
			t.Fatalf("Oracle live key %q value mismatch: got %d, want %d",
				k, v, expectedVal)
		}
	}
}

// Repeated fill, churn, Clear cycles: Len never goes negative, and Len and Range
// are empty right after each Clear.
func TestGache_ClearSettlesCounter(t *testing.T) {
	const cycles = 15
	const numGoroutines = 64

	g := gache.New[int](gache.WithMaxKeyLength[int](8))
	defer g.Stop()

	for cycle := range cycles {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for id := range numGoroutines {
			go func(workerID int) {
				defer wg.Done()
				for i := range 50 {
					k := fmt.Sprintf("cycle_%d_w_%d_k_%d", cycle, workerID, i%10)
					if i%3 == 0 {
						g.Delete(k)
					} else {
						g.Set(k, i)
					}
				}
			}(id)
		}
		wg.Wait()

		// Verify Len >= 0
		l := g.Len()
		if l < 0 {
			t.Fatalf("Cycle %d: Len() is negative (%d)", cycle, l)
		}

		// Clear cache
		g.Clear()

		// Settle check immediately after clear
		postClearLen := g.Len()
		if postClearLen != 0 {
			t.Fatalf("Cycle %d: Len() immediately after Clear() = %d, want 0", cycle, postClearLen)
		}

		var actualCount atomic.Int64
		g.Range(context.Background(), func(k string, v int, exp int64) bool {
			actualCount.Add(1)
			return true
		})
		if n := actualCount.Load(); n != 0 {
			t.Fatalf("Cycle %d: Range found %d items after Clear(), want 0", cycle, n)
		}
	}
}

// Write and Read: gob serialization under concurrent reads and overwrites, and
// deserialization of a corrupt stream returns an error instead of panicking.
func TestGache_WriteReadUnderConcurrentAccess(t *testing.T) {
	t.Run("Concurrent_Write_WithReadsAndOverwrites", func(t *testing.T) {
		gc := gache.New[int]()
		const count = 500
		for i := range count {
			gc.Set(fmt.Sprintf("entry_%04d", i), i)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Concurrent readers
		for range 4 {
			wg.Go(func() {
				for ctx.Err() == nil {
					k := fmt.Sprintf("entry_%04d", rand.Intn(count))
					_, _ = gc.Get(k)
					_ = gc.Len()
				}
			})
		}

		// Concurrent writers updating existing keys
		for range 2 {
			wg.Go(func() {
				for ctx.Err() == nil {
					k := fmt.Sprintf("entry_%04d", rand.Intn(count))
					gc.Set(k, rand.Intn(10000))
				}
			})
		}

		// Writer serializing cache to buffer
		var buf bytes.Buffer
		err := gc.Write(context.Background(), &buf)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		wg.Wait()

		// Read into a new cache instance and verify
		gcRestored := gache.New[int]()
		err = gcRestored.Read(&buf)
		if err != nil {
			t.Fatalf("Read failed to deserialize: %v", err)
		}

		if gcRestored.Len() == 0 {
			t.Fatalf("expected non-empty restored cache, got Len=0")
		}
	})

	t.Run("Read_CorruptedInput_ReturnsErrorWithoutPanic", func(t *testing.T) {
		gc := gache.New[string]()
		corruptedData := []byte("invalid_gob_stream_header_garbage")
		err := gc.Read(bytes.NewReader(corruptedData))
		if err == nil {
			t.Fatalf("expected Read to return error on invalid gob data, got nil")
		}
	})
}

// SetDefaultExpire does not change the TTL of keys stored before it, but applies
// to later writes and to GetRefresh.
func TestGache_SetDefaultExpireAffectsOnlyLaterWrites(t *testing.T) {
	// Initialize with long TTL (1 hour)
	gc := gache.New[string]().SetDefaultExpire(1 * time.Hour)

	gc.Set("pending_long", "should_survive")

	// Dynamically reconfigure default expiration to 20ms
	gc.SetDefaultExpire(20 * time.Millisecond)

	// Insert new key with new default expiration
	gc.Set("new_short", "should_expire_soon")

	// Wait (cache clock) past 20ms: new_short must expire, but pending_long must survive
	gache.WaitCacheElapsed(t, 20*time.Millisecond)

	if _, ok := gc.Get("new_short"); ok {
		t.Fatalf("expected 'new_short' to be expired after its TTL passed on the cache clock, but still found")
	}

	if v, ok := gc.Get("pending_long"); !ok || v != "should_survive" {
		t.Fatalf("expected 'pending_long' to remain alive with its 1h TTL, got val=%s, ok=%v", v, ok)
	}

	// Now call GetRefresh on pending_long: it refreshes using the CURRENT default TTL (20ms)
	v, ok := gc.GetRefresh("pending_long")
	if !ok || v != "should_survive" {
		t.Fatalf("GetRefresh failed for pending_long")
	}

	// Wait (cache clock) past 20ms: pending_long must now be expired because it was refreshed with 20ms TTL!
	gache.WaitCacheElapsed(t, 20*time.Millisecond)

	if _, ok := gc.Get("pending_long"); ok {
		t.Fatalf("expected 'pending_long' to expire after the refreshed TTL passed on the cache clock, but still found")
	}
}

// DeleteExpired against a ground-truth model: exact purge count, gache.NoTTL and unexpired
// entries kept, and the expired hook runs exactly once per evicted key.
func TestGache_DeleteExpiredOracleParity(t *testing.T) {
	const (
		numExpiredPast = 1500 // expired via past timestamp
		numFutureValid = 1500 // future expiration (1 hour)
		numNoTTL       = 1000 // immortal entries (gache.NoTTL)
		numDeletedPre  = 500  // deleted before DeleteExpired
	)

	ctx := context.Background()

	var (
		hookCalls sync.Map
		hookCount atomic.Uint64
	)

	// Configure gache with StartExpired(ctx, 1*time.Hour) so background daemon
	// will drain expChan without triggering periodic tick purges during our test.
	g := gache.New[string]().
		SetDefaultExpire(1*time.Hour).
		SetExpiredHook(func(ctx context.Context, key string, val string) {
			hookCount.Add(1)
			actual, loaded := hookCalls.LoadOrStore(key, 1)
			if loaded {
				hookCalls.Store(key, actual.(int)+1)
			}
		}).
		EnableExpiredHook().
		StartExpired(ctx, 1*time.Hour)
	defer g.Stop()

	// 1. Populate Expired Entries (Category A: expired in past)
	for i := range numExpiredPast {
		k := fmt.Sprintf("catA_exp_%05d", i)
		v := fmt.Sprintf("val_exp_%05d", i)
		// Set with 1ms TTL; it is expired once the cache clock has passed it (waited below)
		g.SetWithExpire(k, v, 1*time.Millisecond)
	}

	// 2. Populate Future Valid Entries (Category B: future TTL)
	for i := range numFutureValid {
		k := fmt.Sprintf("catB_valid_%05d", i)
		v := fmt.Sprintf("val_valid_%05d", i)
		g.SetWithExpire(k, v, 2*time.Hour)
	}

	// 3. Populate gache.NoTTL Entries (Category C: immortal)
	for i := range numNoTTL {
		k := fmt.Sprintf("catC_nottl_%05d", i)
		v := fmt.Sprintf("val_nottl_%05d", i)
		g.SetWithExpire(k, v, gache.NoTTL)
	}

	// 4. Populate Deleted Entries (Category D: inserted then deleted)
	for i := range numDeletedPre {
		k := fmt.Sprintf("catD_del_%05d", i)
		v := fmt.Sprintf("val_del_%05d", i)
		g.Set(k, v)
		g.Delete(k)
	}

	// Ensure past keys have strictly expired (1ms TTL, waited on the cache clock)
	gache.WaitCacheElapsed(t, 1*time.Millisecond)

	// Execute synchronous DeleteExpired
	deletedCount := g.DeleteExpired(ctx)

	if deletedCount != numExpiredPast {
		t.Fatalf("DeleteExpired count mismatch: expected %d, got %d", numExpiredPast, deletedCount)
	}

	// Verify Category A entries: all must be evicted
	for i := range numExpiredPast {
		k := fmt.Sprintf("catA_exp_%05d", i)
		if v, ok := g.Get(k); ok {
			t.Errorf("Expired key %q was not evicted, value=%v", k, v)
		}
	}

	// Verify Category B entries: all must remain accessible
	for i := range numFutureValid {
		k := fmt.Sprintf("catB_valid_%05d", i)
		expectedVal := fmt.Sprintf("val_valid_%05d", i)
		if v, ok := g.Get(k); !ok || v != expectedVal {
			t.Errorf("Valid future key %q missing or corrupted: ok=%v, val=%v", k, ok, v)
		}
	}

	// Verify Category C entries: all must remain accessible
	for i := range numNoTTL {
		k := fmt.Sprintf("catC_nottl_%05d", i)
		expectedVal := fmt.Sprintf("val_nottl_%05d", i)
		if v, ok := g.Get(k); !ok || v != expectedVal {
			t.Errorf("gache.NoTTL key %q missing or corrupted: ok=%v, val=%v", k, ok, v)
		}
	}

	// Verify Category D entries: remained unretrievable
	for i := range numDeletedPre {
		k := fmt.Sprintf("catD_del_%05d", i)
		if v, ok := g.Get(k); ok {
			t.Errorf("Pre-deleted key %q retrievable: val=%v", k, v)
		}
	}

	// Verify Hook Callbacks: wait for queue drain
	gache.WaitUntilProgress(t, "the expired hook to run for every evicted key",
		func() bool { return hookCount.Load() >= uint64(numExpiredPast) },
		func() int { return int(hookCount.Load()) })

	if count := hookCount.Load(); count != uint64(numExpiredPast) {
		t.Fatalf("Hook execution count mismatch: expected %d, got %d", numExpiredPast, count)
	}

	// Check each expired key had callback executed EXACTLY ONCE
	for i := range numExpiredPast {
		k := fmt.Sprintf("catA_exp_%05d", i)
		val, exists := hookCalls.Load(k)
		if !exists {
			t.Errorf("Hook was NOT called for expired key %q", k)
		} else if val.(int) != 1 {
			t.Errorf("Hook called %d times for expired key %q (expected exactly 1)", val.(int), k)
		}
	}

	// Verify NO hook was called for valid or deleted keys
	for i := range numFutureValid {
		k := fmt.Sprintf("catB_valid_%05d", i)
		if _, exists := hookCalls.Load(k); exists {
			t.Errorf("Hook incorrectly called for valid key %q", k)
		}
	}
	for i := range numNoTTL {
		k := fmt.Sprintf("catC_nottl_%05d", i)
		if _, exists := hookCalls.Load(k); exists {
			t.Errorf("Hook incorrectly called for gache.NoTTL key %q", k)
		}
	}
	for i := range numDeletedPre {
		k := fmt.Sprintf("catD_del_%05d", i)
		if _, exists := hookCalls.Load(k); exists {
			t.Errorf("Hook incorrectly called for pre-deleted key %q", k)
		}
	}
}

// churnAllOps drives g for d with goroutines that Set/SetWithExpire, Delete, Pop,
// Range (leaving early), sample Len and Clear the keys, and returns how often Len
// was negative.
func churnAllOps(g gache.Gache[string], keys []string, goroutines int, d time.Duration) (negLen int64) {
	var (
		stop atomic.Bool
		neg  atomic.Int64
		wg   sync.WaitGroup
	)
	for id := range goroutines {
		wg.Go(func() {
			for iter := 0; !stop.Load(); iter++ {
				k := keys[(id+iter)%len(keys)]
				switch id % 8 {
				case 0, 1, 2: // Set / SetWithExpire
					if iter%2 == 0 {
						g.Set(k, fmt.Sprintf("val_%d_%d", id, iter))
					} else {
						g.SetWithExpire(k, fmt.Sprintf("expval_%d_%d", id, iter), time.Hour)
					}
				case 3:
					g.Delete(k)
				case 4:
					g.Pop(k)
				case 5:
					var cnt atomic.Int32 // Range calls f from several goroutines
					g.Range(context.Background(), func(string, string, int64) bool {
						return cnt.Add(1) < 20
					})
				case 6:
					if g.Len() < 0 {
						neg.Add(1)
					}
				default:
					time.Sleep(15 * time.Millisecond)
					g.Clear()
				}
			}
		})
	}
	time.Sleep(d)
	stop.Store(true)
	wg.Wait()
	return neg.Load()
}

// Under every operation at once Len is never negative, and once the cache settles
// Len, Range, Keys, Values and Get agree. The colliding-key rows put every key on
// the same hash (the same shard), the default row spreads them.
func TestGache_ChurnKeepsLenConsistent(t *testing.T) {
	tests := []struct {
		name       string
		keyFmt     string
		opts       []gache.Option[string]
		numKeys    int
		goroutines int
	}{
		{name: "MaxKeyLength=8", opts: []gache.Option[string]{gache.WithMaxKeyLength[string](8)}, keyFmt: "prefix__key_%04d", numKeys: 40, goroutines: 120},
		{name: "MaxKeyLength=4 one shard", opts: []gache.Option[string]{gache.WithMaxKeyLength[string](4)}, keyFmt: "aaaa_colliding_key_%02d", numKeys: 30, goroutines: 120},
		{name: "default hashing", opts: nil, keyFmt: "k_%d", numKeys: 800, goroutines: 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gache.New[string](tt.opts...)
			defer g.Stop()
			keys := make([]string, tt.numKeys)
			for i := range keys {
				keys[i] = fmt.Sprintf(tt.keyFmt, i)
			}

			if neg := churnAllOps(g, keys, tt.goroutines, gache.StressBudget(2*time.Second)); neg != 0 {
				t.Fatalf("Len was negative %d times", neg)
			}

			ctx := context.Background()
			var mu sync.Mutex // Range calls f from several goroutines
			entries := map[string]string{}
			g.Range(ctx, func(k, v string, _ int64) bool {
				mu.Lock()
				entries[k] = v
				mu.Unlock()
				return true
			})
			if n := g.Len(); n != len(entries) {
				t.Fatalf("Len = %d, Range visited %d", n, len(entries))
			}
			if n := len(g.Keys(ctx)); n != len(entries) {
				t.Fatalf("Keys = %d, Range visited %d", n, len(entries))
			}
			if n := len(g.Values(ctx)); n != len(entries) {
				t.Fatalf("Values = %d, Range visited %d", n, len(entries))
			}
			for k, want := range entries {
				if v, ok := g.Get(k); !ok || v != want {
					t.Fatalf("Get(%q) = (%q, %v), Range saw %q", k, v, ok, want)
				}
			}
		})
	}
}

// Keys, Values and ToRawMap racing writers, TTL churn and DeleteExpired sweeps never
// panic or return an unknown, empty or duplicate key, and agree with each other and
// with Len once the writes settle and the short TTLs are purged.
func TestGache_GatherUnderChurn(t *testing.T) {
	const (
		numWriters   = 100
		numReaders   = 20
		numSweepers  = 10
		numKeys      = 1000
		keyPrefix    = "gather_key_"
		maxTTLMillis = 20
	)
	keyUniverse := make([]string, numKeys)
	valid := make(map[string]bool, numKeys)
	for i := range keyUniverse {
		keyUniverse[i] = fmt.Sprintf("%s%05d", keyPrefix, i)
		valid[keyUniverse[i]] = true
	}

	g := gache.New[string](gache.WithMaxKeyLength[string](32), gache.WithMaxWorkers[string](runtime.NumCPU()*2))
	defer g.Stop()
	for i := range 300 {
		g.Set(keyUniverse[i], fmt.Sprintf("val_init_%05d", i))
	}

	var (
		stop       atomic.Bool
		violations atomic.Uint64
		wg         sync.WaitGroup
		ctx        = context.Background()
	)
	for wid := range numWriters {
		wg.Go(func() {
			for seq := uint64(1); !stop.Load(); seq++ {
				k := keyUniverse[(wid*31+int(seq))%numKeys]
				v := fmt.Sprintf("v_%d_%d", wid, seq)
				switch seq % 6 {
				case 0:
					g.Set(k, v)
				case 1:
					ttl := time.Duration(1+seq%maxTTLMillis) * time.Millisecond
					if seq%10 == 0 {
						ttl = gache.NoTTL
					}
					g.SetWithExpire(k, v, ttl)
				case 2:
					g.Delete(k)
				case 3:
					g.Pop(k)
				case 4:
					g.SetIfNotExists(k, v)
				default:
					g.GetRefresh(k)
				}
				if seq%64 == 0 {
					runtime.Gosched()
				}
			}
		})
	}
	for rid := range numReaders {
		wg.Go(func() {
			for iter := 0; !stop.Load(); iter++ {
				switch (iter + rid) % 3 {
				case 0:
					keys := g.Keys(ctx)
					seen := make(map[string]bool, len(keys))
					for _, k := range keys {
						if k == "" || !valid[k] || seen[k] {
							violations.Add(1)
						}
						seen[k] = true
					}
				case 1:
					for _, v := range g.Values(ctx) {
						if v == "" {
							violations.Add(1)
						}
					}
				default:
					for k, v := range g.ToRawMap(ctx) {
						if k == "" || v == "" || !valid[k] {
							violations.Add(1)
						}
					}
				}
				runtime.Gosched()
			}
		})
	}
	for range numSweepers {
		wg.Go(func() {
			for !stop.Load() {
				g.DeleteExpired(ctx)
				runtime.Gosched()
			}
		})
	}

	time.Sleep(gache.StressBudget(2 * time.Second))
	stop.Store(true)
	wg.Wait()

	if n := violations.Load(); n != 0 {
		t.Fatalf("%d empty, unknown or duplicate entries in Keys/Values/ToRawMap snapshots", n)
	}

	gache.WaitCacheElapsed(t, maxTTLMillis*time.Millisecond)
	g.DeleteExpired(ctx)
	keys, vals, raw, n := g.Keys(ctx), g.Values(ctx), g.ToRawMap(ctx), g.Len()
	if len(keys) != len(vals) || len(keys) != len(raw) || len(keys) != n {
		t.Fatalf("settled state disagrees: Keys=%d Values=%d ToRawMap=%d Len=%d", len(keys), len(vals), len(raw), n)
	}
	for _, k := range keys {
		if _, ok := raw[k]; !ok {
			t.Errorf("key %q in Keys is missing from ToRawMap", k)
		}
	}
}

// Every operation, the expiry daemon lifecycle (StartExpired, Stop) and the expired
// hook (Enable/Disable, lazily created channel) racing on one cache: no race, no
// deadlock, no panic, no leaked goroutine, enough throughput, and Clear still
// empties the cache. lazyHookChan starts the workers with no hook set and the
// expiry channel not yet created: SetExpiredHook and EnableExpiredHook (both of
// which create the channel) then race with evictions, StartExpired and
// DeleteExpired on the cache's lazy first initExpChan.
func TestGache_LifecycleChaos(t *testing.T) {
	for _, tc := range []struct {
		name      string
		preEnable bool
		budget    time.Duration
	}{
		{"hookEnabled", true, 3 * time.Second},
		{"lazyHookChan", false, 2500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) { lifecycleChaos(t, tc.preEnable, tc.budget) })
	}
}

func lifecycleChaos(t *testing.T, preEnable bool, fullBudget time.Duration) {
	const (
		goroutines = 64
		numKeys    = 300
	)
	// Keys reaching every getShardID branch under the default maxKeyLength (256):
	// empty and one byte (not hashed), <= 32 bytes (hashShort), 33-64 bytes
	// (hashMid), 65-256 bytes (sampled hashLongKey) and > 256 bytes (trimmed).
	keys := make([]string, numKeys)
	keys[0], keys[1], keys[2], keys[3], keys[4] = "", "a", "z", "\x00", "\xff"
	for i := 5; i < numKeys/2; i++ {
		keys[i] = fmt.Sprintf("short_k_%d", i)
	}
	for i := numKeys / 2; i < numKeys; i++ {
		mid := fmt.Sprintf("long_key_for_wide_hashing_branch_exceeding_32_bytes_node_idx_%d", i) // 64 bytes
		switch {
		case i >= numKeys-10:
			keys[i] = mid + strings.Repeat("t", 300) // > 256: trimmed to maxKeyLength
		case i >= numKeys-50:
			keys[i] = mid + strings.Repeat("s", 36) // 100 bytes: sampled
		default:
			keys[i] = mid
		}
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	var hookCalls atomic.Uint64
	g := gache.New[int](gache.WithDefaultExpiration[int](500 * time.Millisecond))
	hook := func(context.Context, string, int) { hookCalls.Add(1) }
	if preEnable {
		g.SetExpiredHook(hook)
		g.EnableExpiredHook()
	}

	budget := gache.StressBudget(fullBudget)
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	var (
		ops atomic.Uint64
		wg  sync.WaitGroup
	)
	start := make(chan struct{})
	for wid := range goroutines {
		wg.Go(func() {
			<-start
			rng := rand.New(rand.NewSource(int64(wid*1000 + 42)))
			sub := func(d time.Duration) (context.Context, context.CancelFunc) { return context.WithTimeout(ctx, d) }
			for iter := 1; ctx.Err() == nil; iter++ {
				k := keys[rng.Intn(numKeys)]
				switch rng.Intn(16) {
				case 0:
					g.Set(k, wid*100000+iter)
				case 1:
					if v, ok := g.Get(k); ok && v < 0 {
						t.Errorf("worker %d: negative value %d for key %q", wid, v, k)
					}
				case 2:
					g.Delete(k)
				case 3:
					g.Pop(k)
				case 4:
					g.SetIfNotExists(k, wid*100000+iter)
				case 5:
					g.ExtendExpire(k, time.Duration(rng.Intn(20)+1)*time.Millisecond)
				case 6:
					g.GetRefresh(k)
				case 7:
					c, cancel := sub(50 * time.Millisecond)
					if g.Keys(c) == nil {
						t.Errorf("worker %d: Keys returned nil", wid)
					}
					cancel()
				case 8:
					c, cancel := sub(50 * time.Millisecond)
					if g.Values(c) == nil {
						t.Errorf("worker %d: Values returned nil", wid)
					}
					cancel()
				case 9:
					c, cancel := sub(50 * time.Millisecond)
					if g.ToRawMap(c) == nil {
						t.Errorf("worker %d: ToRawMap returned nil", wid)
					}
					cancel()
				case 10:
					g.Clear()
				case 11:
					c, cancel := sub(15 * time.Millisecond)
					g.StartExpired(c, 5*time.Millisecond)
					time.Sleep(100 * time.Microsecond)
					cancel()
				case 12:
					g.Stop()
				case 13:
					c, cancel := sub(50 * time.Millisecond)
					g.DeleteExpired(c)
					cancel()
				case 14:
					if !preEnable {
						g.SetExpiredHook(hook)
					}
					g.EnableExpiredHook()
				default:
					g.DisableExpiredHook()
				}
				ops.Add(1)
				if iter%128 == 0 {
					runtime.Gosched()
				}
			}
		})
	}
	close(start)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(budget + 10*time.Second):
		t.Fatal("deadlock: workers did not finish within 10s of the deadline")
	}
	g.Stop()
	t.Logf("%d operations, %d hook calls", ops.Load(), hookCalls.Load())
	// Liveness floor of 10000 ops per 3s of budget (scaled down for shorter runs): an op that blocks for most of the run without
	// deadlocking outright fails here.
	minOps := uint64(10000 * budget / (3 * time.Second))
	if gache.RaceDetectorEnabled {
		// The race detector slows every op 5-10x (8.6k ops in 3s observed under
		// go test -race -count=3); a stalled op still drops far below a tenth.
		minOps /= 10
	}
	if ops.Load() < minOps {
		t.Errorf("only %d operations in %v, want >= %d", ops.Load(), budget, minOps)
	}

	var finalGoroutines int
	for range 20 {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		if finalGoroutines = runtime.NumGoroutine(); finalGoroutines <= initialGoroutines+2 {
			break
		}
	}
	if finalGoroutines > initialGoroutines+2 {
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		t.Errorf("goroutine leak: initial=%d final=%d", initialGoroutines, finalGoroutines)
	}

	g.Clear()
	if n := g.Len(); n != 0 {
		t.Errorf("Len after Clear = %d, want 0", n)
	}
}

// New is two allocations (the gache and its shard arena) within a fixed footprint,
// and the hot paths (Get, GetWithExpire, in-place Set) allocate nothing. Not
// parallel: testing.AllocsPerRun needs a quiet process.
func TestGache_Allocations(t *testing.T) {
	t.Run("New", func(t *testing.T) {
		allocs := testing.AllocsPerRun(100, func() { _ = gache.New[int]() })
		// The race detector's shadow memory pollutes malloc counts.
		if !gache.RaceDetectorEnabled && allocs > 2 {
			t.Errorf("gache.New[int]() made %.1f allocations, want <= 2", allocs)
		}

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		const count = 50
		holders := make([]gache.Gache[int], count)
		for i := range holders {
			holders[i] = gache.New[int]()
		}
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(holders)
		if per := (after.TotalAlloc - before.TotalAlloc) / count; per > 350*1024 {
			t.Errorf("memory per instance = %d KB, want <= 350 KB", per/1024)
		}
		if gache.New[int]().Size() == 0 {
			t.Error("Size of an empty cache = 0")
		}
	})

	t.Run("hot paths", func(t *testing.T) {
		g := gache.New[int]()
		const key = "test_hot_path_key"
		g.Set(key, 42)
		for name, f := range map[string]func(){
			"Get": func() {
				if v, ok := g.Get(key); !ok || v != 42 && v != 100 {
					t.Fatalf("Get = (%d, %v)", v, ok)
				}
			},
			"GetWithExpire": func() {
				if _, exp, ok := g.GetWithExpire(key); !ok || exp <= 0 {
					t.Fatalf("GetWithExpire = (%d, %v)", exp, ok)
				}
			},
			"Set overwrite": func() { g.Set(key, 100) },
		} {
			if n := testing.AllocsPerRun(1000, f); n > 0 {
				t.Errorf("%s allocated %v allocs/op, want 0", name, n)
			}
		}
	})
}

// ExtendExpire racing a Delete/Set of the same key must neither panic nor revive a
// deleted entry: the node it found can be tombstoned before ExtendExpire locks it.
func TestGache_ExtendExpireRacesDelete(t *testing.T) {
	t.Parallel()

	g := gache.New[int]()
	var (
		stop atomic.Bool
		wg   sync.WaitGroup
	)
	for range 4 {
		wg.Go(func() {
			for !stop.Load() {
				g.ExtendExpire("k", time.Minute)
			}
		})
	}
	for range 2 {
		wg.Go(func() {
			for i := 0; !stop.Load(); i++ {
				g.Set("k", i)
				g.Delete("k")
			}
		})
	}
	time.Sleep(gache.StressBudget(300 * time.Millisecond))
	stop.Store(true)
	wg.Wait()

	g.Delete("k")
	g.ExtendExpire("k", time.Minute)
	if v, ok := g.Get("k"); ok {
		t.Errorf("Get after Delete and ExtendExpire = (%d, true), want the key absent", v)
	}
}
