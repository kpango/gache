package gache

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// One row per option function of option.go: the field it sets on a gache built by
// New. WithDefaultExpiration and WithDefaultExpirationString also behave as
// SetDefaultExpire does, which TestOption_WithDefaultExpiration checks through the
// public API.
func TestOption_Fields(t *testing.T) {
	t.Parallel()

	hook := func(context.Context, string, int) {}
	tests := []struct {
		check func(t *testing.T, g *gache[int])
		name  string
		opts  []Option[int]
	}{
		{name: "defaults", opts: nil, check: func(t *testing.T, g *gache[int]) {
			if g.expire != int64(30*time.Second) || g.maxKeyLength != 256 || g.maxWorkers != runtime.NumCPU()*2 || g.expFuncEnabled.Load() {
				t.Errorf("defaults = expire %d, maxKeyLength %d, maxWorkers %d, hook enabled %v", g.expire, g.maxKeyLength, g.maxWorkers, g.expFuncEnabled.Load())
			}
		}},
		{name: "WithDefaultExpiration", opts: []Option[int]{WithDefaultExpiration[int](time.Hour)}, check: func(t *testing.T, g *gache[int]) {
			if g.expire != int64(time.Hour) {
				t.Errorf("expire = %d, want %d", g.expire, int64(time.Hour))
			}
		}},
		{name: "WithDefaultExpirationString", opts: []Option[int]{WithDefaultExpirationString[int]("5m")}, check: func(t *testing.T, g *gache[int]) {
			if g.expire != int64(5*time.Minute) {
				t.Errorf("expire = %d, want %d", g.expire, int64(5*time.Minute))
			}
		}},
		{name: "WithExpiredHookFunc", opts: []Option[int]{WithExpiredHookFunc(hook)}, check: func(t *testing.T, g *gache[int]) {
			if g.expFunc.Load() == nil || !g.expFuncEnabled.Load() {
				t.Errorf("hook set = %v, enabled = %v, want both", g.expFunc.Load() != nil, g.expFuncEnabled.Load())
			}
		}},
		{name: "WithExpiredHookFunc nil is ignored", opts: []Option[int]{WithExpiredHookFunc[int](nil)}, check: func(t *testing.T, g *gache[int]) {
			if g.expFunc.Load() != nil || g.expFuncEnabled.Load() {
				t.Error("a nil hook was installed")
			}
		}},
		{name: "WithMaxKeyLength", opts: []Option[int]{WithMaxKeyLength[int](8)}, check: func(t *testing.T, g *gache[int]) {
			if g.maxKeyLength != 8 {
				t.Errorf("maxKeyLength = %d, want 8", g.maxKeyLength)
			}
		}},
		{name: "WithMaxKeyLength 0 is no limit", opts: []Option[int]{WithMaxKeyLength[int](0)}, check: func(t *testing.T, g *gache[int]) {
			if g.maxKeyLength != 0 {
				t.Errorf("maxKeyLength = %d, want 0", g.maxKeyLength)
			}
		}},
		{name: "WithMaxWorkers", opts: []Option[int]{WithMaxWorkers[int](3)}, check: func(t *testing.T, g *gache[int]) {
			if g.maxWorkers != 3 {
				t.Errorf("maxWorkers = %d, want 3", g.maxWorkers)
			}
		}},
		{name: "later options win", opts: []Option[int]{WithMaxWorkers[int](3), WithMaxWorkers[int](5), WithMaxKeyLength[int](4), WithMaxKeyLength[int](9)}, check: func(t *testing.T, g *gache[int]) {
			if g.maxWorkers != 5 || g.maxKeyLength != 9 {
				t.Errorf("maxWorkers = %d, maxKeyLength = %d, want 5 and 9", g.maxWorkers, g.maxKeyLength)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := New[int](tt.opts...).(*gache[int])
			defer g.Stop()
			tt.check(t, g)
		})
	}
}

// A worker limit of zero, below zero or far above GOMAXPROCS still gathers every
// key (Keys) and keeps Len.
func TestOption_WithMaxWorkersRange(t *testing.T) {
	t.Parallel()

	for _, w := range []int{-10, 0, 1, 1024} {
		g := New[int](WithMaxWorkers[int](w))
		for i := range 100 {
			g.Set(string(rune('a'+i%26))+string(rune('A'+i/26)), i)
		}
		if n := len(g.Keys(context.Background())); n != 100 || g.Len() != 100 {
			t.Errorf("workers %d: Keys = %d, Len = %d, want 100", w, n, g.Len())
		}
		g.Stop()
	}
}

// A default expiration <= 0 disables expiry, exactly as SetDefaultExpire does; a
// positive one sets the TTL; an empty or invalid duration string leaves the 30s
// default.
func TestOption_WithDefaultExpiration(t *testing.T) {
	t.Parallel()

	if err := WithDefaultExpirationString[int]("bogus")(new(gache[int])); err == nil {
		t.Error("an unparsable duration string returned no error")
	}
	if err := WithDefaultExpirationString[int]("500ms")(new(gache[int])); err != nil {
		t.Errorf("a valid duration string returned %v", err)
	}

	tests := []struct {
		opt     Option[int]
		name    string
		ref     time.Duration // the SetDefaultExpire argument that must behave the same
		wantTTL bool
	}{
		{name: "NoTTL", opt: WithDefaultExpiration[int](NoTTL), ref: NoTTL},
		{name: "zero", opt: WithDefaultExpiration[int](0), ref: 0},
		{name: "negative", opt: WithDefaultExpiration[int](-time.Hour), ref: -time.Hour},
		{name: "positive", opt: WithDefaultExpiration[int](time.Hour), ref: time.Hour, wantTTL: true},
		{name: "string 0s", opt: WithDefaultExpirationString[int]("0s"), ref: 0},
		{name: "string negative", opt: WithDefaultExpirationString[int]("-5m"), ref: -5 * time.Minute},
		{name: "string positive", opt: WithDefaultExpirationString[int]("1h"), ref: time.Hour, wantTTL: true},
		{name: "string empty keeps the default", opt: WithDefaultExpirationString[int](""), ref: 30 * time.Second, wantTTL: true},
		{name: "string invalid keeps the default", opt: WithDefaultExpirationString[int]("bogus"), ref: 30 * time.Second, wantTTL: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := New[int](tt.opt)
			g.Set("k", 1)
			v, expire, ok := g.GetWithExpire("k")
			if !ok || v != 1 {
				t.Fatalf("GetWithExpire = (%d, %d, %v), want (1, _, true)", v, expire, ok)
			}
			if got := expire > 0; got != tt.wantTTL {
				t.Fatalf("expire = %d: has a TTL = %v, want %v", expire, got, tt.wantTTL)
			}

			ref := New[int]().SetDefaultExpire(tt.ref)
			ref.Set("k", 1)
			_, refExpire, _ := ref.GetWithExpire("k")
			if !tt.wantTTL && expire != refExpire {
				t.Errorf("expire = %d, SetDefaultExpire(%v) gives %d", expire, tt.ref, refExpire)
			}

			if !tt.wantTTL {
				// Nothing to sweep, and a refresh to the default keeps the entry TTL-less.
				if n := g.DeleteExpired(context.Background()); n != 0 {
					t.Errorf("DeleteExpired removed %d entries, want 0", n)
				}
				if _, ok := g.GetRefresh("k"); !ok {
					t.Fatal("GetRefresh lost the entry")
				}
				if _, e, _ := g.GetWithExpire("k"); e > 0 {
					t.Errorf("expire = %d after GetRefresh, want an entry without TTL", e)
				}
			}
		})
	}
}
