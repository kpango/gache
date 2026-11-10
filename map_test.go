// Copyright (c) 2009 The Go Authors. All rights resered.
// Modified <Yusuke Kato (kpango)>

//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.

// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package gache_test

import (
	"fmt"
	"maps"
	"math/rand"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	gache "github.com/kpango/gache/v2"
)

// Black-box tests of the public Map[K, V] API. Layout: the quick.CheckEqual
// oracle tests against the sync.Map-derived references (map_reference_test.go),
// the contract matrix (every method x value kind x entry state), the fuzz test,
// then the concurrency regression tests. White-box Map tests (node state bits,
// dirty-node release, stPtr barrier, Size internals) live in shardmap_test.go and
// size_test.go.

// mapStressBudget is the wall-clock length of one duration-bounded Map stress run
// (gache.StressBudget shortens it under -short).
func mapStressBudget() time.Duration {
	return gache.StressBudget(300 * time.Millisecond)
}

// runWorkers runs n goroutines that call f in a loop for d, then stops and joins them.
func runWorkers(n int, d time.Duration, f func(id int, r *rand.Rand)) {
	var (
		stop atomic.Bool
		wg   sync.WaitGroup
	)
	for id := range n {
		wg.Go(func() {
			r := rand.New(rand.NewSource(int64(id)))
			for !stop.Load() {
				f(id, r)
			}
		})
	}
	time.Sleep(d)
	stop.Store(true)
	wg.Wait()
}

type mapOp string

const (
	opLoad             = mapOp("Load")
	opStore            = mapOp("Store")
	opLoadOrStore      = mapOp("LoadOrStore")
	opLoadAndDelete    = mapOp("LoadAndDelete")
	opDelete           = mapOp("Delete")
	opSwap             = mapOp("Swap")
	opCompareAndSwap   = mapOp("CompareAndSwap")
	opCompareAndDelete = mapOp("CompareAndDelete")
)

var mapOps = [...]mapOp{
	opLoad,
	opStore,
	opLoadOrStore,
	opLoadAndDelete,
	opDelete,
	opSwap,
	opCompareAndSwap,
	opCompareAndDelete,
}

type mapCall struct {
	k  any
	v  any
	op mapOp
}

func (c mapCall) apply(m mapInterface) (any, bool) {
	switch c.op {
	case opLoad:
		return m.Load(c.k)
	case opStore:
		m.Store(c.k, c.v)
		return nil, false
	case opLoadOrStore:
		return m.LoadOrStore(c.k, c.v)
	case opLoadAndDelete:
		return m.LoadAndDelete(c.k)
	case opDelete:
		m.Delete(c.k)
		return nil, false
	case opSwap:
		return m.Swap(c.k, c.v)
	case opCompareAndSwap:
		if m.CompareAndSwap(c.k, c.v, rand.Int()) {
			m.Delete(c.k)
			return c.v, true
		}
		return nil, false
	case opCompareAndDelete:
		if m.CompareAndDelete(c.k, c.v) {
			if _, ok := m.Load(c.k); !ok {
				return nil, true
			}
		}
		return nil, false
	default:
		panic("invalid mapOp")
	}
}

type mapResult struct {
	value any
	ok    bool
}

func randValue(r *rand.Rand) any {
	b := make([]byte, r.Intn(4))
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func (mapCall) Generate(r *rand.Rand, size int) reflect.Value {
	c := mapCall{op: mapOps[rand.Intn(len(mapOps))], k: randValue(r)}
	switch c.op {
	case opStore, opLoadOrStore:
		c.v = randValue(r)
	}
	return reflect.ValueOf(c)
}

func applyCalls(m mapInterface, calls []mapCall) (results []mapResult, final map[any]any) {
	for _, c := range calls {
		v, ok := c.apply(m)
		results = append(results, mapResult{v, ok})
	}

	final = make(map[any]any)
	m.Range(func(k, v any) bool {
		final[k] = v
		return true
	})

	return results, final
}

func applyMap(calls []mapCall) ([]mapResult, map[any]any) {
	return applyCalls(new(gache.Map[any, any]), calls)
}

func applyRWMutexMap(calls []mapCall) ([]mapResult, map[any]any) {
	return applyCalls(new(RWMutexMap), calls)
}

func applyDeepCopyMap(calls []mapCall) ([]mapResult, map[any]any) {
	return applyCalls(new(DeepCopyMap), calls)
}

// TestMap_MatchesReferences runs randomized call sequences against Map and the
// sync.Map-derived references: a sync.RWMutex-guarded map and a deep-copied map.
func TestMap_MatchesReferences(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		ref  func([]mapCall) ([]mapResult, map[any]any)
		name string
	}{
		{name: "RWMutexMap", ref: applyRWMutexMap},
		{name: "DeepCopyMap", ref: applyDeepCopyMap},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := quick.CheckEqual(applyMap, tt.ref, nil); err != nil {
				t.Error(err)
			}
		})
	}
}

// ---- contract matrix ------------------------------------------------------

const (
	contractKey   = "k"
	contractOther = "z"
)

// contractArgs are the operands an op may use: cur is the value stored under
// contractKey (zero if absent), eq an equal but distinct copy of it, peer a value
// that differs from cur yet belongs to the same kind (nil vs empty for slices,
// nil vs non-nil for pointers), nw a fresh value and wrong an unrelated one.
type contractArgs[V any] struct{ cur, eq, peer, nw, wrong V }

type contractOp[V any] struct {
	name string
	do   func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) (got, want []any)
}

func refCAS[V any](ref map[string]V, old, nw V) bool {
	cur, ok := ref[contractKey]
	if !ok || !reflect.DeepEqual(cur, old) {
		return false
	}
	ref[contractKey] = nw
	return true
}

func refCAD[V any](ref map[string]V, old V) bool {
	cur, ok := ref[contractKey]
	if !ok || !reflect.DeepEqual(cur, old) {
		return false
	}
	delete(ref, contractKey)
	return true
}

// valueContractOps lists every value-API method; each op applies itself to the Map and to
// the plain-map reference and returns both result lists for comparison.
func valueContractOps[V any]() []contractOp[V] {
	cas := func(name string, old func(contractArgs[V]) V, nw func(contractArgs[V]) V) contractOp[V] {
		return contractOp[V]{name, func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			o, n := old(a), nw(a)
			return []any{m.CompareAndSwap(contractKey, o, n)}, []any{refCAS(ref, o, n)}
		}}
	}
	cad := func(name string, old func(contractArgs[V]) V) contractOp[V] {
		return contractOp[V]{name, func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			o := old(a)
			return []any{m.CompareAndDelete(contractKey, o)}, []any{refCAD(ref, o)}
		}}
	}
	eq := func(a contractArgs[V]) V { return a.eq }
	wrong := func(a contractArgs[V]) V { return a.wrong }
	peer := func(a contractArgs[V]) V { return a.peer }
	nw := func(a contractArgs[V]) V { return a.nw }

	return []contractOp[V]{
		{"Load", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			gv, gok := m.Load(contractKey)
			wv, wok := ref[contractKey]
			return []any{gv, gok}, []any{wv, wok}
		}},
		{"Store", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			m.Store(contractKey, a.nw)
			ref[contractKey] = a.nw
			return nil, nil
		}},
		{"Store unrelated value", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			m.Store(contractKey, a.wrong)
			ref[contractKey] = a.wrong
			return nil, nil
		}},
		{"LoadOrStore", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			gv, gl := m.LoadOrStore(contractKey, a.nw)
			if cur, ok := ref[contractKey]; ok {
				return []any{gv, gl}, []any{cur, true}
			}
			ref[contractKey] = a.nw
			return []any{gv, gl}, []any{a.nw, false}
		}},
		{"LoadAndDelete", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			gv, gl := m.LoadAndDelete(contractKey)
			wv, wl := ref[contractKey]
			delete(ref, contractKey)
			return []any{gv, gl}, []any{wv, wl}
		}},
		{"Delete", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			m.Delete(contractKey)
			delete(ref, contractKey)
			return nil, nil
		}},
		{"Swap", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			gv, gl := m.Swap(contractKey, a.nw)
			wv, wl := ref[contractKey]
			ref[contractKey] = a.nw
			return []any{gv, gl}, []any{wv, wl}
		}},
		cas("CompareAndSwap equal copy", eq, nw),
		cas("CompareAndSwap equal copy to unrelated", eq, wrong),
		cas("CompareAndSwap wrong old", wrong, nw),
		cas("CompareAndSwap peer old", peer, nw),
		cad("CompareAndDelete equal copy", eq),
		cad("CompareAndDelete wrong old", wrong),
		cad("CompareAndDelete peer old", peer),
		{"Range", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			got := map[string]V{}
			n := 0
			m.Range(func(k string, v V) bool { got[k] = v; n++; return true })
			return []any{got, n}, []any{maps.Clone(ref), len(ref)}
		}},
		{"Clear", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			m.Clear()
			clear(ref)
			return nil, nil
		}},
		{"Len", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			return []any{m.Len()}, []any{len(ref)}
		}},
		{"Size", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			return []any{m.Size() > 0}, []any{true}
		}},
		{"InitReserve", func(m *gache.Map[string, V], ref map[string]V, a contractArgs[V]) ([]any, []any) {
			m.InitReserve(64) // pre-sizes an empty map and is a no-op otherwise
			return nil, nil
		}},
	}
}

// contractScripts are the entry states: one letter per step applied to contractKey.
// s stores v0, S stores v1, d deletes, p promotes the dirty map by a Range, c clears,
// x stores and deletes another key (rebuilding the dirty map, which expunges
// tombstones of the read snapshot). Empty is absent.
var contractScripts = []struct{ name, script string }{
	{"absent", ""},
	{"live", "s"},
	{"live promoted", "sp"},
	{"deleted", "sd"},
	{"deleted promoted", "spd"},
	{"deleted expunged", "spdx"},
	{"re-stored", "sdS"},
	{"re-stored promoted", "spdS"},
	{"re-stored after expunge", "spdxS"},
	{"cleared", "sc"},
}

// scriptSteps are the effects of the contractScripts letters on the Map under test and
// its reference: s and S store the first and second value under contractKey, d deletes
// it, p promotes the dirty map, c clears, x stores and deletes another key.
type scriptSteps struct{ s, S, d, p, c, x func() }

func runSteps(script string, st scriptSteps) {
	for _, step := range script {
		switch step {
		case 's':
			st.s()
		case 'S':
			st.S()
		case 'd':
			st.d()
		case 'p':
			st.p()
		case 'c':
			st.c()
		case 'x':
			st.x()
		}
	}
}

func runScript[V any](m *gache.Map[string, V], ref map[string]V, script string, v0, v1, spare V) {
	store := func(v V) func() {
		return func() { m.Store(contractKey, v); ref[contractKey] = v }
	}
	runSteps(script, scriptSteps{
		s: store(v0),
		S: store(v1),
		d: func() { m.Delete(contractKey); delete(ref, contractKey) },
		p: func() { m.Range(func(string, V) bool { return true }) },
		c: func() { m.Clear(); clear(ref) },
		x: func() { m.Store("scratch", spare); m.Delete("scratch") },
	})
}

// checkContractState asserts the whole observable state of m against ref with one
// assertion set, whichever op ran last.
func checkContractState[V any](t *testing.T, m *gache.Map[string, V], ref map[string]V) {
	t.Helper()
	for _, k := range []string{contractKey, contractOther, "absent"} {
		gv, gok := m.Load(k)
		wv, wok := ref[k]
		if gok != wok || !reflect.DeepEqual(gv, wv) {
			t.Fatalf("state: Load(%q) = (%v, %v), want (%v, %v)", k, gv, gok, wv, wok)
		}
		if _, pok := m.LoadPointer(k); pok != wok {
			t.Fatalf("state: LoadPointer(%q) ok = %v, want %v", k, pok, wok)
		}
	}
	if got := m.Len(); got != len(ref) {
		t.Fatalf("state: Len = %d, want %d", got, len(ref))
	}
	seen := map[string]V{}
	m.Range(func(k string, v V) bool {
		if _, dup := seen[k]; dup {
			t.Fatalf("state: Range visited %q twice", k)
		}
		seen[k] = v
		return true
	})
	if !reflect.DeepEqual(seen, ref) {
		t.Fatalf("state: Range = %v, want %v", seen, ref)
	}
	n := 0
	m.RangePointer(func(string, *V) bool { n++; return true })
	if n != len(ref) {
		t.Fatalf("state: RangePointer visited %d, want %d", n, len(ref))
	}
	if m.Size() == 0 {
		t.Fatal("state: Size = 0")
	}
}

// runValueContract applies every value-API method to every entry state, with and
// without an unrelated entry (an empty map takes the lock-free empty fast paths),
// and compares the results and the resulting state with a plain map. v holds, in
// order, the live value, the re-stored value, a fresh value and an unrelated one.
func runValueContract[V any](t *testing.T, v [4]V, clone func(V) V) {
	t.Helper()
	for _, withOther := range []bool{false, true} {
		for _, st := range contractScripts {
			for _, op := range valueContractOps[V]() {
				t.Run(fmt.Sprintf("other=%v/%s/%s", withOther, st.name, op.name), func(t *testing.T) {
					var m gache.Map[string, V]
					ref := map[string]V{}
					if withOther {
						m.Store(contractOther, v[2])
						ref[contractOther] = v[2]
					}
					runScript(&m, ref, st.script, v[0], v[1], v[2])
					checkContractState(t, &m, ref) // the entry state itself
					cur := ref[contractKey]
					peer := v[1]
					if c, ok := ref[contractKey]; ok && reflect.DeepEqual(c, v[1]) {
						peer = v[0]
					}
					got, want := op.do(&m, ref, contractArgs[V]{cur: cur, eq: clone(cur), peer: peer, nw: v[2], wrong: v[3]})
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("result = %v, want %v", got, want)
					}
					checkContractState(t, &m, ref)
				})
			}
		}
	}
}

// TestMap_Contract is the contract matrix: all value-API methods x value kind x
// entry state, one consistent assertion set (results, Len, Load, Range, Size)
// checked against a plain map after every op.
func TestMap_Contract(t *testing.T) {
	t.Parallel()

	t.Run("int", func(t *testing.T) {
		t.Parallel()
		runValueContract(t, [4]int{11, 22, 33, 44}, func(v int) int { return v })
	})
	t.Run("string", func(t *testing.T) {
		t.Parallel()
		runValueContract(t, [4]string{"alpha", "beta", "gamma", "delta"}, func(v string) string { return v })
	})
	t.Run("[4]int64", func(t *testing.T) {
		t.Parallel()
		runValueContract(t, [4][4]int64{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}, {13, 14, 15, 16}}, func(v [4]int64) [4]int64 { return v })
	})
	t.Run("*int including nil", func(t *testing.T) {
		t.Parallel()
		i0, i2, i3 := 1, 3, 9
		// v1 is nil: the re-stored state holds a nil pointer value. DeepEqual
		// compares the pointees, so an equal copy matches and nil matches only nil.
		runValueContract(t, [4]*int{&i0, nil, &i2, &i3}, func(p *int) *int {
			if p == nil {
				return nil
			}
			q := *p
			return &q
		})
	})
	t.Run("[]int nil vs empty", func(t *testing.T) {
		t.Parallel()
		// v0 is empty and v1 is nil: DeepEqual tells them apart.
		runValueContract(t, [4][]int{{}, nil, {1, 2}, {1}}, func(s []int) []int {
			if s == nil {
				return nil
			}
			return append([]int{}, s...)
		})
	})
}

// pointerOp is one Pointer-API op; got and want are compared by identity.
type pointerOp struct {
	name string
	do   func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) (got, want []any)
}

type pointerArgs struct{ cur, copy, p2, other *int }

// refSetPtr models the Pointer API rule on the reference: storing a nil pointer
// deletes the entry.
func refSetPtr(ref map[string]*int, p *int) {
	if p == nil {
		delete(ref, contractKey)
		return
	}
	ref[contractKey] = p
}

func pointerOps() []pointerOp {
	cas := func(name string, old func(pointerArgs) *int, nw func(pointerArgs) *int) pointerOp {
		return pointerOp{name, func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			o, n := old(a), nw(a)
			got := m.CompareAndSwapPointer(contractKey, o, n)
			cur, ok := ref[contractKey]
			want := ok && cur == o
			if want {
				refSetPtr(ref, n)
			}
			return []any{got}, []any{want}
		}}
	}
	cur := func(a pointerArgs) *int { return a.cur }
	cp := func(a pointerArgs) *int { return a.copy }
	nilp := func(pointerArgs) *int { return nil }
	other := func(a pointerArgs) *int { return a.other }
	p2 := func(a pointerArgs) *int { return a.p2 }

	store := func(name string, p func(pointerArgs) *int) pointerOp {
		return pointerOp{name, func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			m.StorePointer(contractKey, p(a))
			refSetPtr(ref, p(a))
			return nil, nil
		}}
	}
	loadOrStore := func(name string, p func(pointerArgs) *int) pointerOp {
		return pointerOp{name, func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			gp, gl := m.LoadOrStorePointer(contractKey, p(a))
			if c, ok := ref[contractKey]; ok {
				return []any{gp, gl}, []any{c, true}
			}
			refSetPtr(ref, p(a))
			return []any{gp, gl}, []any{p(a), false}
		}}
	}
	swap := func(name string, p func(pointerArgs) *int) pointerOp {
		return pointerOp{name, func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			gp, gl := m.SwapPointer(contractKey, p(a))
			wp, wl := ref[contractKey]
			refSetPtr(ref, p(a))
			return []any{gp, gl}, []any{wp, wl}
		}}
	}

	return []pointerOp{
		{"LoadPointer", func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			gp, gok := m.LoadPointer(contractKey)
			wp, wok := ref[contractKey]
			return []any{gp, gok}, []any{wp, wok}
		}},
		store("StorePointer", p2),
		store("StorePointer nil deletes", nilp),
		loadOrStore("LoadOrStorePointer", p2),
		loadOrStore("LoadOrStorePointer nil", nilp),
		swap("SwapPointer", p2),
		swap("SwapPointer nil deletes", nilp),
		{"LoadAndDeletePointer", func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			gp, gl := m.LoadAndDeletePointer(contractKey)
			wp, wl := ref[contractKey]
			delete(ref, contractKey)
			return []any{gp, gl}, []any{wp, wl}
		}},
		cas("CompareAndSwapPointer identical", cur, p2),
		cas("CompareAndSwapPointer identical to nil deletes", cur, nilp),
		cas("CompareAndSwapPointer equal content, other pointer", cp, p2),
		cas("CompareAndSwapPointer nil old", nilp, p2),
		cas("CompareAndSwapPointer unrelated old", other, p2),
		{"RangePointer", func(m *gache.Map[string, int], ref map[string]*int, a pointerArgs) ([]any, []any) {
			got := map[string]*int{}
			m.RangePointer(func(k string, p *int) bool { got[k] = p; return true })
			var gs, ws []any
			for _, k := range []string{contractKey, contractOther} {
				gs = append(gs, got[k])
				ws = append(ws, ref[k])
			}
			return append(gs, len(got)), append(ws, len(ref))
		}},
	}
}

// TestMap_PointerContract is the contract matrix of the Pointer API: pointer
// identity (never content) is preserved by every method and entry state,
// including a promotion of the dirty map, and nil pointers delete.
func TestMap_PointerContract(t *testing.T) {
	t.Parallel()

	for _, withOther := range []bool{false, true} {
		for _, st := range contractScripts {
			for _, op := range pointerOps() {
				t.Run(fmt.Sprintf("other=%v/%s/%s", withOther, st.name, op.name), func(t *testing.T) {
					t.Parallel()
					var m gache.Map[string, int]
					ref := map[string]*int{}
					p0, p1, p2, otherP, oth := new(int), new(int), new(int), new(int), new(int)
					*p0, *p1 = 7, 8
					if withOther {
						m.StorePointer(contractOther, oth)
						ref[contractOther] = oth
					}
					storeP := func(p *int) func() {
						return func() { m.StorePointer(contractKey, p); ref[contractKey] = p }
					}
					runSteps(st.script, scriptSteps{
						s: storeP(p0),
						S: storeP(p1),
						d: func() { m.Delete(contractKey); delete(ref, contractKey) },
						p: func() { m.RangePointer(func(string, *int) bool { return true }) },
						c: func() { m.Clear(); clear(ref) },
						x: func() { m.StorePointer("scratch", oth); m.Delete("scratch") },
					})
					cur := ref[contractKey]
					cp := new(int)
					if cur != nil {
						*cp = *cur
					}
					got, want := op.do(&m, ref, pointerArgs{cur: cur, copy: cp, p2: p2, other: otherP})
					if !slices.Equal(got, want) {
						t.Fatalf("result = %v, want %v", got, want)
					}
					for _, k := range []string{contractKey, contractOther} {
						gp, gok := m.LoadPointer(k)
						wp, wok := ref[k]
						if gp != wp || gok != wok {
							t.Fatalf("state: LoadPointer(%q) = (%p, %v), want (%p, %v)", k, gp, gok, wp, wok)
						}
					}
					if got := m.Len(); got != len(ref) {
						t.Fatalf("state: Len = %d, want %d", got, len(ref))
					}
				})
			}
		}
	}
}

// TestMap_StoreCopiesValue pins that Store boxes a copy: a pointer obtained before
// an overwrite keeps the old content, and the overwrite hands out a new pointer.
func TestMap_StoreCopiesValue(t *testing.T) {
	t.Parallel()

	var m gache.Map[string, int]
	m.Store("k", 1)
	p1, ok := m.LoadPointer("k")
	if !ok || p1 == nil || *p1 != 1 {
		t.Fatalf("LoadPointer after Store(1) = (%v, %v)", p1, ok)
	}
	m.Store("k", 2)
	if *p1 != 1 {
		t.Fatalf("pointer from the first Store changed to %d", *p1)
	}
	if p2, _ := m.LoadPointer("k"); p2 == p1 || *p2 != 2 {
		t.Fatalf("second LoadPointer = (%p -> %d), want a distinct pointer to 2", p2, *p2)
	}
}

// TestMap_CompareAndSwapKeepsPointerOnMismatch pins that a failed CompareAndSwap
// leaves the stored pointer itself (not just an equal value) in place.
func TestMap_CompareAndSwapKeepsPointerOnMismatch(t *testing.T) {
	t.Parallel()

	var m gache.Map[string, int]
	m.Store("k", 1)
	before, _ := m.LoadPointer("k")
	if m.CompareAndSwap("k", 2, 3) {
		t.Fatal("CompareAndSwap with a different value succeeded")
	}
	if after, _ := m.LoadPointer("k"); after != before {
		t.Fatalf("stored pointer changed from %p to %p", before, after)
	}
}

// TestMap_CompareAndSwapNonExistingKey: a CompareAndSwap on a key that was never
// stored (here a *Map used as an any key) must report false and store nothing.
func TestMap_CompareAndSwapNonExistingKey(t *testing.T) {
	t.Parallel()

	m := &gache.Map[any, any]{}
	if m.CompareAndSwap(m, nil, 42) {
		t.Fatalf("CompareAndSwap on an non-existing key succeeded")
	}
}

// ---- key types, range, size -----------------------------------------------

type mapStructKey struct {
	Name string
	ID   int
}

// checkKeyType exercises one key type end to end; keys must be pairwise distinct.
func checkKeyType[K comparable](t *testing.T, keys []K) {
	t.Helper()
	var m gache.Map[K, int]
	for i, k := range keys {
		m.Store(k, i)
	}
	if got := m.Len(); got != len(keys) {
		t.Fatalf("Len = %d, want %d", got, len(keys))
	}
	for i, k := range keys {
		if v, ok := m.Load(k); !ok || v != i {
			t.Fatalf("Load(%#v) = (%d, %v), want (%d, true)", k, v, ok, i)
		}
	}
	seen := make(map[K]int, len(keys))
	m.Range(func(k K, v int) bool {
		if _, dup := seen[k]; dup {
			t.Fatalf("Range visited %#v twice", k)
		}
		seen[k] = v
		return true
	})
	if len(seen) != len(keys) {
		t.Fatalf("Range visited %d keys, want %d", len(seen), len(keys))
	}
	for i, k := range keys {
		if seen[k] != i {
			t.Fatalf("Range value for %#v = %d, want %d", k, seen[k], i)
		}
	}
	for i, k := range keys {
		if i%2 == 0 {
			if v, loaded := m.LoadAndDelete(k); !loaded || v != i {
				t.Fatalf("LoadAndDelete(%#v) = (%d, %v), want (%d, true)", k, v, loaded, i)
			}
		}
	}
	for i, k := range keys {
		_, ok := m.Load(k)
		if want := i%2 == 1; ok != want {
			t.Fatalf("Load(%#v) ok = %v after deleting evens, want %v", k, ok, want)
		}
	}
	if got, want := m.Len(), len(keys)/2; got != want {
		t.Fatalf("Len after deleting evens = %d, want %d", got, want)
	}
	for i, k := range keys {
		if i%2 == 1 && !m.CompareAndSwap(k, i, -i) {
			t.Fatalf("CompareAndSwap(%#v) failed", k)
		}
	}
	for i, k := range keys {
		if i%2 == 1 && !m.CompareAndDelete(k, -i) {
			t.Fatalf("CompareAndDelete(%#v) failed", k)
		}
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("Len after deleting everything = %d, want 0", got)
	}
}

// TestMap_KeyTypes also pins that an any key keeps its dynamic type: 1, int64(1),
// int32(1) and "1" are distinct keys.
func TestMap_KeyTypes(t *testing.T) {
	t.Parallel()

	ptr := new(int)

	t.Run("int", func(t *testing.T) {
		t.Parallel()
		keys := make([]int, 0, 200)
		for i := -100; i < 100; i++ {
			keys = append(keys, i)
		}
		checkKeyType(t, keys)
	})
	t.Run("struct", func(t *testing.T) {
		t.Parallel()
		keys := make([]mapStructKey, 0, 100)
		for i := range 100 {
			keys = append(keys, mapStructKey{Name: fmt.Sprint("n", i%7), ID: i})
		}
		checkKeyType(t, keys)
	})
	t.Run("pointer", func(t *testing.T) {
		t.Parallel()
		keys := make([]*int, 50)
		for i := range keys {
			keys[i] = new(int)
		}
		checkKeyType(t, keys)
	})
	t.Run("array", func(t *testing.T) {
		t.Parallel()
		keys := make([][2]int, 0, 50)
		for i := range 50 {
			keys = append(keys, [2]int{i, -i})
		}
		checkKeyType(t, keys)
	})
	t.Run("any with mixed dynamic types", func(t *testing.T) {
		t.Parallel()
		checkKeyType(t, []any{
			1, int64(1), int32(1), uint8(1), "1", 1.0, float32(1), true, false,
			mapStructKey{Name: "1", ID: 1},
			[2]int{1, 1},
			ptr, nil,
			struct{}{},
			'x', "", 0,
		})
	})
}

// TestMap_RangeEarlyStopAndSkip: Range and RangePointer stop when f returns false
// and never visit deleted entries.
func TestMap_RangeEarlyStopAndSkip(t *testing.T) {
	t.Parallel()

	var m gache.Map[int, int]
	for i := range 100 {
		m.Store(i, i)
	}
	var calls int
	m.Range(func(int, int) bool { calls++; return false })
	if calls != 1 {
		t.Fatalf("Range called f %d times after it returned false, want 1", calls)
	}
	calls = 0
	m.RangePointer(func(int, *int) bool { calls++; return calls < 10 })
	if calls != 10 {
		t.Fatalf("RangePointer called f %d times, want 10", calls)
	}
	for i := range 100 {
		if i%3 != 0 {
			m.Delete(i)
		}
	}
	got := map[int]int{}
	m.Range(func(k, v int) bool { got[k] = v; return true })
	for i := range 100 {
		_, ok := got[i]
		if want := i%3 == 0; ok != want {
			t.Fatalf("Range visited key %d = %v, want %v", i, ok, want)
		}
	}
}

func TestMap_SizeGrows(t *testing.T) {
	t.Parallel()

	var m gache.Map[int, int]
	empty := m.Size()
	if empty == 0 {
		t.Fatal("empty Size = 0, want > 0")
	}
	prev := empty
	for _, n := range []int{10, 100, 1000} {
		for i := range n {
			m.Store(i, i)
		}
		if got := m.Size(); got <= prev {
			t.Fatalf("Size with %d entries = %d, want > %d", n, got, prev)
		}
		prev = m.Size()
	}
}

// ---- fuzz -------------------------------------------------------------------

// FuzzMap_MatchesReference decodes the input into (op, key, value) byte triples
// and applies each to a Map[uint8, int] and to a plain map, comparing every
// return value and, after each op, Len, Range and the loaded value of every key.
func FuzzMap_MatchesReference(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 3, 7, 0, 3, 0, 3, 3, 0, 0, 3, 0})                   // store, load, load-and-delete, load
	f.Add([]byte{1, 1, 5, 6, 1, 0x85, 6, 1, 0x85, 7, 1, 0x85, 0, 1, 0}) // CompareAndSwap / CompareAndDelete sequence
	f.Add([]byte{1, 2, 9, 8, 0, 0, 9, 0, 0, 1, 2, 4, 0, 2, 0})          // clear, promote, re-store
	f.Add([]byte{1, 4, 1, 5, 4, 2, 2, 4, 3, 10, 4, 0, 11, 4, 1, 11, 4, 0})
	seed := make([]byte, 3*600)
	rand.New(rand.NewSource(1)).Read(seed)
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		var m gache.Map[uint8, int]
		ref := map[uint8]int{}
		for i := 0; i+2 < len(data); i += 3 {
			op, k, b := data[i]%12, data[i+1]%16, data[i+2]
			v := int(b)
			fail := func(what string, got, want any) {
				t.Helper()
				t.Fatalf("op %d (code %d) key %d val %d: %s = %v, want %v", i/3, op, k, v, what, got, want)
			}
			rv, rok := ref[k]
			switch op {
			case 0:
				if gv, gok := m.Load(k); gv != rv || gok != rok {
					fail("Load", [2]any{gv, gok}, [2]any{rv, rok})
				}
			case 1:
				m.Store(k, v)
				ref[k] = v
			case 2:
				gv, gl := m.LoadOrStore(k, v)
				wv, wl := v, false
				if rok {
					wv, wl = rv, true
				} else {
					ref[k] = v
				}
				if gv != wv || gl != wl {
					fail("LoadOrStore", [2]any{gv, gl}, [2]any{wv, wl})
				}
			case 3:
				gv, gl := m.LoadAndDelete(k)
				delete(ref, k)
				if gv != rv || gl != rok {
					fail("LoadAndDelete", [2]any{gv, gl}, [2]any{rv, rok})
				}
			case 4:
				m.Delete(k)
				delete(ref, k)
			case 5:
				gv, gl := m.Swap(k, v)
				ref[k] = v
				if gv != rv || gl != rok {
					fail("Swap", [2]any{gv, gl}, [2]any{rv, rok})
				}
			case 6, 7:
				old := v
				if b&0x80 != 0 {
					old = rv // a hit whenever the key is present
				}
				if op == 6 {
					want := rok && rv == old
					if want {
						ref[k] = v + 1000
					}
					if got := m.CompareAndSwap(k, old, v+1000); got != want {
						fail("CompareAndSwap", got, want)
					}
				} else {
					want := rok && rv == old
					if want {
						delete(ref, k)
					}
					if got := m.CompareAndDelete(k, old); got != want {
						fail("CompareAndDelete", got, want)
					}
				}
			case 8:
				if b < 64 {
					m.Clear()
					clear(ref)
				} else {
					m.Range(func(uint8, int) bool { return false }) // stop at the first entry
				}
			case 9:
				n := 0
				m.Range(func(uint8, int) bool { n++; return n < int(b%4)+1 })
				if want := min(len(ref), int(b%4)+1); n != want {
					fail("early-stopped Range visits", n, want)
				}
			case 10:
				p, ok := m.LoadPointer(k)
				if ok != rok || (ok && *p != rv) {
					fail("LoadPointer", [2]any{p, ok}, [2]any{rv, rok})
				}
				if ok {
					nv := v + 2000
					if !m.CompareAndSwapPointer(k, p, &nv) {
						fail("CompareAndSwapPointer", false, true)
					}
					ref[k] = nv
				}
			case 11:
				if b&1 == 0 {
					m.StorePointer(k, nil)
					delete(ref, k)
				} else {
					nv := v
					gp, gl := m.LoadOrStorePointer(k, &nv)
					if gl != rok || (gl && *gp != rv) || (!gl && *gp != nv) {
						fail("LoadOrStorePointer", [2]any{*gp, gl}, [2]any{rv, rok})
					}
					if !rok {
						ref[k] = nv
					}
				}
			}

			if got := m.Len(); got != len(ref) {
				fail("Len", got, len(ref))
			}
			seen := map[uint8]int{}
			m.Range(func(k uint8, v int) bool { seen[k] = v; return true })
			if !reflect.DeepEqual(seen, ref) {
				fail("Range", seen, ref)
			}
			for key := range uint8(16) {
				gv, gok := m.Load(key)
				wv, wok := ref[key]
				if gv != wv || gok != wok {
					fail(fmt.Sprintf("Load(%d) after op", key), [2]any{gv, gok}, [2]any{wv, wok})
				}
			}
		}
	})
}

// ---- concurrency regression tests ---------------------------------------------

func testMapExpungeGC[V any](t *testing.T, val1, val2 V) {
	t.Helper()

	var m gache.Map[string, V]

	// Store, promote by a Load miss path, delete (tombstone) and store a new key
	// (which expunges the tombstoned node), then force GC while the expunged node
	// is only reachable from the old read snapshot and the live node boxes a V of
	// this type: the GC must trace both, whatever the layout of V.
	m.Store("k1", val1)
	m.Load("k1")
	m.Delete("k1")
	m.Store("k2", val2)
	for range 3 {
		runtime.GC()
	}

	if _, ok := m.Load("k1"); ok {
		t.Errorf("k1 should be deleted")
	}
	if _, ok := m.Load("k2"); !ok {
		t.Errorf("k2 should exist")
	}
}

// TestMap_ExpungedVariousTypes sequentially checks that expunged nodes survive GC
// across a wide spectrum of value layouts.
func TestMap_ExpungedVariousTypes(t *testing.T) {
	t.Run("int", func(t *testing.T) { testMapExpungeGC[int](t, 1, 2) })
	t.Run("string", func(t *testing.T) { testMapExpungeGC[string](t, "a", "b") })

	i1, i2 := 1, 2
	t.Run("*int", func(t *testing.T) { testMapExpungeGC[*int](t, &i1, &i2) })

	t.Run("[]byte", func(t *testing.T) { testMapExpungeGC[[]byte](t, []byte("a"), []byte("b")) })

	t.Run("map", func(t *testing.T) {
		testMapExpungeGC[map[string]int](t, map[string]int{"a": 1}, map[string]int{"b": 2})
	})

	type SmallStruct struct{ a, b int }
	t.Run("SmallStruct", func(t *testing.T) {
		testMapExpungeGC[SmallStruct](t, SmallStruct{1, 2}, SmallStruct{3, 4})
	})

	type BigStruct struct {
		arr [1000]int64
		p   *int
		m   map[string]int
	}
	t.Run("BigStruct", func(t *testing.T) {
		testMapExpungeGC[BigStruct](t, BigStruct{p: &i1}, BigStruct{p: &i2})
	})

	type ChannelStruct chan bool
	t.Run("ChannelStruct", func(t *testing.T) {
		testMapExpungeGC[ChannelStruct](t, make(chan bool), make(chan bool))
	})
}

// TestMap_Issue40999 is the golang/go#40999 regression: a deleted entry must not
// keep its key reachable, so a finalizer on a stored-then-deleted key runs.
func TestMap_Issue40999(t *testing.T) {
	var m gache.Map[any, any]

	m.Store(nil, struct{}{})

	var finalized atomic.Uint32
	for deadline := time.Now().Add(30 * time.Second); finalized.Load() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("finalizer of a deleted key never ran: the map retains it")
		}
		p := new(int)
		runtime.SetFinalizer(p, func(*int) {
			finalized.Add(1)
		})
		m.Store(p, struct{}{})
		m.Delete(p)
		runtime.GC()
	}
}

// TestMap_ConcurrentRange sweeps the map with Range while writers overwrite its
// values: every sweep must see each key exactly once, with a value from a valid write.
func TestMap_ConcurrentRange(t *testing.T) {
	const mapSize = 1 << 10

	m := new(gache.Map[any, any])
	for n := int64(1); n <= mapSize; n++ {
		m.Store(n, int64(n))
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()
	for g := int64(runtime.GOMAXPROCS(0)); g > 0; g-- {
		r := rand.New(rand.NewSource(g))
		wg.Go(func() {
			for i := int64(0); ; i++ {
				select {
				case <-done:
					return
				default:
				}
				for n := int64(1); n < mapSize; n++ {
					if r.Int63n(mapSize) == 0 {
						m.Store(n, n*i*g)
					} else {
						m.Load(n)
					}
				}
			}
		})
	}

	deadline := time.Now().Add(3 * mapStressBudget())
	for n := 0; n < 4 || time.Now().Before(deadline); n++ {
		seen := make(map[int64]bool, mapSize)

		m.Range(func(ki, vi any) bool {
			k, v := ki.(int64), vi.(int64)
			if v%k != 0 {
				t.Fatalf("while Storing multiples of %v, Range saw value %v", k, v)
			}
			if seen[k] {
				t.Fatalf("Range visited key %v twice", k)
			}
			seen[k] = true
			return true
		})

		if len(seen) != mapSize {
			t.Fatalf("Range visited %v elements of %v-element Map", len(seen), mapSize)
		}
	}
}

// TestMap_RangeNestedCall: Range callbacks may call Range and every other method
// on the same map, including deleting the entry being visited.
func TestMap_RangeNestedCall(t *testing.T) {
	var m gache.Map[any, any]
	for i, v := range [3]string{"hello", "world", "Go"} {
		m.Store(i, v)
	}
	m.Range(func(key, value any) bool {
		m.Range(func(key, value any) bool {
			if v, ok := m.Load(key); !ok || !reflect.DeepEqual(v, value) {
				t.Fatalf("Nested Range loads unexpected value, got %+v want %+v", v, value)
			}

			if _, loaded := m.LoadOrStore(42, "dummy"); loaded {
				t.Fatalf("Nested Range loads unexpected value, want store a new value")
			}

			val := "gache.Map[any, any]"
			m.Store(42, val)
			if v, loaded := m.LoadAndDelete(42); !loaded || !reflect.DeepEqual(v, val) {
				t.Fatalf("Nested Range loads unexpected value, got %v, want %v", v, val)
			}
			return true
		})

		m.Delete(key)
		return true
	})

	length := 0
	m.Range(func(key, value any) bool {
		length++
		return true
	})

	if length != 0 {
		t.Fatalf("Unexpected gache.Map[any, any] size, got %v want %v", length, 0)
	}
}

// assertLenMatchesRange checks that a quiescent map's Len equals what Range visits.
func assertLenMatchesRange(t *testing.T, m *gache.Map[int, int]) {
	t.Helper()
	actual := 0
	m.Range(func(int, int) bool { actual++; return true })
	if got := m.Len(); got != actual {
		t.Fatalf("Len() = %d, Range counted %d entries", got, actual)
	}
}

// TestMap_LenConcurrent: after concurrent mutation the length counter matches
// what Range visits (the counter must not desynchronize from the entries).
func TestMap_LenConcurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		run  func(t *testing.T, m *gache.Map[int, int])
		name string
	}{
		{name: "mixed ops on a small key range", run: func(t *testing.T, m *gache.Map[int, int]) {
			const keyRange = 200
			runWorkers(16, mapStressBudget(), func(id int, r *rand.Rand) {
				key, i := r.Intn(keyRange), r.Int()
				switch r.Intn(6) {
				case 0:
					m.Store(key, i)
				case 1:
					m.LoadOrStore(key, i)
				case 2:
					m.Swap(key, i)
				case 3:
					m.Delete(key)
				case 4:
					m.LoadAndDelete(key)
				case 5:
					m.CompareAndDelete(key, i)
				}
			})
			assertLenMatchesRange(t, m)
		}},
		{name: "Clear racing writers and deleters", run: func(t *testing.T, m *gache.Map[int, int]) {
			var (
				stop atomic.Bool
				wg   sync.WaitGroup
			)
			for id := range 8 {
				wg.Go(func() {
					r := rand.New(rand.NewSource(int64(id)))
					for !stop.Load() {
						if id < 4 {
							m.Store(r.Intn(100), id)
						} else {
							m.Delete(r.Intn(100))
						}
					}
				})
			}
			for deadline := time.Now().Add(mapStressBudget()); time.Now().Before(deadline); {
				m.Clear()
				if l := m.Len(); l < 0 {
					t.Fatalf("Len() = %d after Clear, must not be negative", l)
				}
			}
			stop.Store(true)
			wg.Wait()
			assertLenMatchesRange(t, m)
		}},
		{name: "disjoint stores and deletes end empty", run: func(t *testing.T, m *gache.Map[int, int]) {
			const goroutines, keysPer = 8, 500
			var wg sync.WaitGroup
			for g := range goroutines {
				wg.Go(func() {
					for i := range keysPer {
						m.Store(g*keysPer+i, i)
					}
				})
				wg.Go(func() {
					for i := range keysPer {
						m.Delete(g*keysPer + i)
					}
				})
			}
			wg.Wait()
			m.Range(func(k, v int) bool { m.Delete(k); return true })
			if got := m.Len(); got != 0 {
				t.Fatalf("after storing and deleting all keys, Len() = %d, want 0", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, new(gache.Map[int, int]))
		})
	}
}

// TestMap_SameKeyChurnLenMatchesRange: after bursts of same-key revive/delete races,
// Len equals the number of entries Range visits (no TTL involved). The Gache
// counterpart is TestGache_SameKeyChurnLenMatchesRange.
func TestMap_SameKeyChurnLenMatchesRange(t *testing.T) {
	t.Parallel()

	for _, churnKeys := range []int{16, 4096} {
		t.Run(fmt.Sprintf("churnKeys=%d", churnKeys), func(t *testing.T) {
			t.Parallel()
			gache.SameKeyRounds(func(n int) {
				var m gache.Map[int, int]
				for k := range 4 {
					m.Store(k, k)
				}
				gache.SameKeyChurn(churnKeys,
					m.Store, func(k, _ int) { m.Delete(k) },
					m.Store, func(k, _ int) { m.Delete(k) })
				mr := 0
				m.Range(func(int, int) bool { mr++; return true })
				if m.Len() != mr {
					t.Fatalf("round %d: Map.Len = %d, Map.Range visited %d", n, m.Len(), mr)
				}
			})
		})
	}
}

// TestMap_ReviveNeverExposesNilPointer: a lock-free LoadPointer racing a
// StorePointer that revives a tombstoned node must never report the key present
// with a nil (or different) pointer: the reviver has to publish the payload before
// the node becomes live. The shardMap-level variant is
// TestShardMap_ReviveNeverExposesNilPointer (white box).
func TestMap_ReviveNeverExposesNilPointer(t *testing.T) {
	t.Parallel()

	p := new(int)
	*p = 5
	var m gache.Map[string, int]
	hits, bad := gache.RunReviveChurn(
		func() { m.StorePointer("k", p) },
		func() { m.Delete("k") },
		func() (bool, bool) { v, ok := m.LoadPointer("k"); return ok, v == p },
	)
	if bad != 0 {
		t.Fatalf("%d of %d LoadPointer hits returned a pointer other than the stored one", bad, hits)
	}
	if hits == 0 {
		t.Fatal("readers never observed the key; the race was not exercised")
	}
}

// singleCASWinner races many goroutines on CompareAndSwap(k, zero, next(id)) for a
// duration (at least 200 rounds); exactly one may win each round.
func singleCASWinner[V any](t *testing.T, zero V, next func(id int) V) {
	t.Helper()
	racers := max(16, 2*runtime.GOMAXPROCS(0))
	deadline := time.Now().Add(mapStressBudget())
	for round := 0; round < 200 || time.Now().Before(deadline); round++ {
		var m gache.Map[string, V]
		m.Store("k", zero)
		var (
			wg   sync.WaitGroup
			wins atomic.Int64
		)
		start := make(chan struct{})
		for id := 1; id <= racers; id++ {
			wg.Go(func() {
				<-start
				if m.CompareAndSwap("k", zero, next(id)) {
					wins.Add(1)
				}
			})
		}
		close(start)
		wg.Wait()
		if got := wins.Load(); got != 1 {
			t.Fatalf("round %d: %d goroutines won the CAS, want exactly 1", round, got)
		}
	}
}

// TestMap_ConcurrentCAS: the CompareAnd*/LoadOrStore/Swap family hands out each
// state transition to exactly one caller under contention (this also pins the
// under-lock re-check of CompareAndSwap, which only shows under contention).
func TestMap_ConcurrentCAS(t *testing.T) {
	t.Parallel()

	const goroutines, perG = 16, 500

	t.Run("counter increments exactly once per success", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		m.Store("n", 0)
		var wg sync.WaitGroup
		var successes atomic.Int64
		for range goroutines {
			wg.Go(func() {
				for range perG {
					for {
						old, ok := m.Load("n")
						if !ok {
							t.Error("counter key vanished")
							return
						}
						if m.CompareAndSwap("n", old, old+1) {
							successes.Add(1)
							break
						}
					}
				}
			})
		}
		wg.Wait()
		if got, want := successes.Load(), int64(goroutines*perG); got != want {
			t.Fatalf("successful CAS count = %d, want %d", got, want)
		}
		if got, _ := m.Load("n"); got != goroutines*perG {
			t.Fatalf("counter = %d, want %d", got, goroutines*perG)
		}
		if got := m.Len(); got != 1 {
			t.Fatalf("Len = %d, want 1", got)
		}
	})

	t.Run("pointer counter increments exactly once per success", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		m.Store("n", 0)
		var wg sync.WaitGroup
		for range goroutines {
			wg.Go(func() {
				for range perG {
					for {
						old, ok := m.LoadPointer("n")
						if !ok {
							t.Error("counter key vanished")
							return
						}
						next := *old + 1
						if m.CompareAndSwapPointer("n", old, &next) {
							break
						}
					}
				}
			})
		}
		wg.Wait()
		if got, _ := m.Load("n"); got != goroutines*perG {
			t.Fatalf("counter = %d, want %d", got, goroutines*perG)
		}
	})

	// Many rounds of a barrier-released race: every goroutine passes the lock-free
	// pre-check of CompareAndSwap together, so only the re-check under the node lock
	// keeps the winner unique (a mutant that drops it lets several win). The large
	// value makes the DeepEqual between the pre-check and the lock slow, which
	// widens the window.
	t.Run("single CAS winner", func(t *testing.T) {
		t.Parallel()
		singleCASWinner(t, 0, func(id int) int { return id })
	})
	t.Run("single CAS winner, large value", func(t *testing.T) {
		t.Parallel()
		singleCASWinner(t, [2048]int{}, func(id int) [2048]int { return [2048]int{id} })
	})

	t.Run("single LoadOrStore storer", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		var wg sync.WaitGroup
		var stored atomic.Int64
		for id := 1; id <= goroutines; id++ {
			wg.Go(func() {
				if _, loaded := m.LoadOrStore("k", id); !loaded {
					stored.Add(1)
				}
			})
		}
		wg.Wait()
		if got := stored.Load(); got != 1 {
			t.Fatalf("%d goroutines stored, want exactly 1", got)
		}
		if got := m.Len(); got != 1 {
			t.Fatalf("Len = %d, want 1", got)
		}
	})

	t.Run("single LoadAndDelete winner", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		m.Store("k", 7)
		var wg sync.WaitGroup
		var wins atomic.Int64
		for range goroutines {
			wg.Go(func() {
				if v, loaded := m.LoadAndDelete("k"); loaded {
					if v != 7 {
						t.Errorf("LoadAndDelete value = %d, want 7", v)
					}
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		if got := wins.Load(); got != 1 {
			t.Fatalf("%d goroutines deleted the key, want exactly 1", got)
		}
		if got := m.Len(); got != 0 {
			t.Fatalf("Len = %d, want 0", got)
		}
	})

	t.Run("single CompareAndDelete winner", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		m.Store("k", 7)
		var wg sync.WaitGroup
		var wins atomic.Int64
		for range goroutines {
			wg.Go(func() {
				if m.CompareAndDelete("k", 7) {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		if got := wins.Load(); got != 1 {
			t.Fatalf("%d goroutines deleted the key, want exactly 1", got)
		}
		if got := m.Len(); got != 0 {
			t.Fatalf("Len = %d, want 0", got)
		}
	})

	t.Run("swap hands every previous value to exactly one caller", func(t *testing.T) {
		t.Parallel()
		var m gache.Map[string, int]
		m.Store("k", 0)
		var wg sync.WaitGroup
		prevs := make([]int, goroutines)
		for id := 1; id <= goroutines; id++ {
			wg.Go(func() {
				prev, loaded := m.Swap("k", id)
				if !loaded {
					t.Error("Swap on a present key reported loaded=false")
				}
				prevs[id-1] = prev
			})
		}
		wg.Wait()
		last, _ := m.Load("k")
		// The values handed back plus the final value are exactly {0..goroutines}.
		seen := map[int]int{last: 1}
		for _, p := range prevs {
			seen[p]++
		}
		if len(seen) != goroutines+1 {
			t.Fatalf("previous values are not a permutation: %v (last=%d)", prevs, last)
		}
		for v, n := range seen {
			if n != 1 || v < 0 || v > goroutines {
				t.Fatalf("value %d seen %d times: %v (last=%d)", v, n, prevs, last)
			}
		}
	})
}

// TestMap_PointerValuesSurviveConcurrentGC: a *V stored through Store, StorePointer
// or LoadOrStorePointer stays visible to the GC (the node word is written with a
// barriered pointer store), so no reader ever sees a reclaimed or overwritten payload.
func TestMap_PointerValuesSurviveConcurrentGC(t *testing.T) {
	t.Parallel()

	const keys = 16
	var (
		m     gache.Map[int, gache.GCPayload]
		round atomic.Uint64
	)
	// corrupt is a payload that fails Valid, reported for any bad pointee seen by
	// the LoadOrStorePointer and RangePointer paths.
	corrupt := new(gache.GCPayload)
	gache.GCStress(t,
		func(k int, p *gache.GCPayload) {
			if round.Add(1)%2 == 0 {
				m.StorePointer(k, p)
			} else {
				m.Store(k, *p)
			}
		},
		func(k int) { m.Delete(k) },
		func(k int) *gache.GCPayload {
			p, _ := m.LoadPointer(k)
			if q, _ := m.LoadOrStorePointer((k+1)%keys, gache.NewGCPayload(1, 0)); q == nil || !q.Valid() {
				return corrupt
			}
			bad := false
			m.RangePointer(func(_ int, p *gache.GCPayload) bool {
				bad = bad || !p.Valid()
				return true
			})
			if bad {
				return corrupt
			}
			return p
		},
	)
}
