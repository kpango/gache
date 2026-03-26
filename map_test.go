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
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"unsafe"

	gache "github.com/kpango/gache/v2"
)

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

var testExpunged uint64

func testExpungedPtr[V any]() *V {
	return (*V)(unsafe.Pointer(&testExpunged))
}

type MyStruct struct {
	a, b int64
}

func testMapExpungeGC[V any](t *testing.T, val1, val2 V) {
	t.Helper()

	var m gache.Map[string, V]

	// 1. Store k1 -> populates dirty map, read.amended = true
	m.Store("k1", val1)

	// 2. Load k1 -> triggers missLocked(), promotes dirty to read, dirty = nil
	m.Load("k1")

	// 3. Delete k1 -> sets entry pointer to nil in read map
	m.Delete("k1")

	// 4. Store k2 -> since dirty is nil and amended is false, calls dirtyLocked().
	// dirtyLocked iterates over read map, finds k1 is nil, and uses
	// CompareAndSwap(nil, expungedPtr[V]()) to mark it expunged.
	m.Store("k2", val2)

	// 5. Force GC to trace the expunged global pointer as various generic types.
	// If expungedGlobal wasn't safe (e.g., straddling allocations, bad alignment,
	// or the GC trying to scan inside it as a large struct), this would crash.
	for range 3 {
		runtime.GC()
	}

	// 6. Verify cache state
	if _, ok := m.Load("k1"); ok {
		t.Errorf("k1 should be deleted")
	}
	if _, ok := m.Load("k2"); !ok {
		t.Errorf("k2 should exist")
	}
}

// TestMap_Checkptr ensures that global expunged pointers correctly survive garbage collection when utilized across primitive generic types.
func TestMap_Checkptr(t *testing.T) {
	p := testExpungedPtr[MyStruct]()
	if p == nil {
		t.Fail()
	}
}

// TestMap_CheckptrBig ensures that global expunged pointers correctly survive garbage collection when interacting with oversized generic structures.
func TestMap_CheckptrBig(t *testing.T) {
	type Big struct {
		a, b, c, d int64
	}
	p := testExpungedPtr[Big]()
	if p == nil {
		t.Fail()
	}
}

// TestMap_CompareAndSwap_NonExistingKey validates that CompareAndSwap safely rejects mutation attempts targeting non-existent key slots.
func TestMap_CompareAndSwap_NonExistingKey(t *testing.T) {
	m := &gache.Map[any, any]{}
	if m.CompareAndSwap(m, nil, 42) {
		t.Fatalf("CompareAndSwap on an non-existing key succeeded")
	}
}

// TestMap_ConcurrentRange stresses the map by performing overlapping iteration sweeps while concurrent goroutines mutate the map topology.
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

	iters := 1 << 10
	if testing.Short() {
		iters = 16
	}
	for n := iters; n > 0; n-- {
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

// TestMap_ExpungedVariousTypes sequentially checks the map's capacity to safely expunge unlinked key slots across a wide spectrum of variable types.
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

// TestMap_Issue40999 serves as a regression test verifying that finalizers execute correctly for map keys avoiding memory leaks.
func TestMap_Issue40999(t *testing.T) {
	var m gache.Map[any, any]

	m.Store(nil, struct{}{})

	var finalized uint32

	for atomic.LoadUint32(&finalized) == 0 {
		p := new(int)
		runtime.SetFinalizer(p, func(*int) {
			atomic.AddUint32(&finalized, 1)
		})
		m.Store(p, struct{}{})
		m.Delete(p)
		runtime.GC()
	}
}

// TestMap_LenBasic checks the map's raw item count reliability through basic sequential lifecycle operations.
func TestMap_LenBasic(t *testing.T) {
	var m gache.Map[string, int]

	if got := m.Len(); got != 0 {
		t.Fatalf("empty map Len() = %d, want 0", got)
	}

	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)
	if got := m.Len(); got != 3 {
		t.Fatalf("after 3 Stores, Len() = %d, want 3", got)
	}

	m.Store("b", 20)
	if got := m.Len(); got != 3 {
		t.Fatalf("after overwrite, Len() = %d, want 3", got)
	}

	m.Delete("a")
	if got := m.Len(); got != 2 {
		t.Fatalf("after Delete, Len() = %d, want 2", got)
	}

	m.Delete("nonexistent")
	if got := m.Len(); got != 2 {
		t.Fatalf("after Delete(nonexistent), Len() = %d, want 2", got)
	}

	if _, ok := m.LoadAndDelete("b"); !ok {
		t.Fatal("LoadAndDelete(b) returned ok=false")
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("after LoadAndDelete, Len() = %d, want 1", got)
	}

	if _, loaded := m.LoadOrStore("d", 4); loaded {
		t.Fatal("LoadOrStore(d) returned loaded=true for new key")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("after LoadOrStore(new), Len() = %d, want 2", got)
	}

	if _, loaded := m.LoadOrStore("d", 40); !loaded {
		t.Fatal("LoadOrStore(d) returned loaded=false for existing key")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("after LoadOrStore(existing), Len() = %d, want 2", got)
	}

	if _, loaded := m.Swap("e", 5); loaded {
		t.Fatal("Swap(e) returned loaded=true for new key")
	}
	if got := m.Len(); got != 3 {
		t.Fatalf("after Swap(new), Len() = %d, want 3", got)
	}

	if _, loaded := m.Swap("e", 50); !loaded {
		t.Fatal("Swap(e) returned loaded=false for existing key")
	}
	if got := m.Len(); got != 3 {
		t.Fatalf("after Swap(existing), Len() = %d, want 3", got)
	}

	if !m.CompareAndDelete("e", 50) {
		t.Fatal("CompareAndDelete(e, 50) returned false")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("after CompareAndDelete, Len() = %d, want 2", got)
	}

	if m.CompareAndDelete("c", 999) {
		t.Fatal("CompareAndDelete with wrong value returned true")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("after CompareAndDelete(wrong), Len() = %d, want 2", got)
	}

	m.Clear()
	if got := m.Len(); got != 0 {
		t.Fatalf("after Clear, Len() = %d, want 0", got)
	}
}

// TestMap_LenClearConcurrent confirms that the map's internal counters do not desynchronize when subjected to aggressive concurrent clear calls.
func TestMap_LenClearConcurrent(t *testing.T) {
	var m gache.Map[int, int]

	const (
		numWriters  = 4
		numDeleters = 4
		ops         = 500
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
					m.Store(r.Intn(100), id)
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
					m.Delete(r.Intn(100))
				}
			}
		})
	}

	for range ops {
		m.Clear()
		if l := m.Len(); l < 0 {
			t.Fatalf("Len() = %d after Clear, must not be negative", l)
		}
	}

	close(done)
	wg.Wait()

	actual := 0
	m.Range(func(k, v int) bool {
		actual++
		return true
	})
	if got := m.Len(); got != actual {
		t.Fatalf("final Len() = %d, Range counted %d", got, actual)
	}
}

// TestMap_LenConcurrent tests the integrity of the atomic length tracker when bombarded with unpredictable multi-threaded mutation.
func TestMap_LenConcurrent(t *testing.T) {
	var m gache.Map[int, int]

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
				key := r.Intn(keyRange)
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
			}
		})
	}
	wg.Wait()

	actual := 0
	m.Range(func(k, v int) bool {
		actual++
		return true
	})

	if got := m.Len(); got != actual {
		t.Fatalf("after concurrent ops, Len() = %d, but Range counted %d entries", got, actual)
	}
}

// TestMap_LenConcurrentStoreDelete ensures length consistency when an equal volume of targeted stores and deletes run completely in parallel.
func TestMap_LenConcurrentStoreDelete(t *testing.T) {
	var m gache.Map[int, int]

	const (
		numGoroutines    = 8
		keysPerGoroutine = 500
	)

	var wg sync.WaitGroup

	for g := range numGoroutines {
		wg.Go(func() {
			base := g * keysPerGoroutine
			for i := range keysPerGoroutine {
				m.Store(base+i, i)
			}
		})
	}

	for g := range numGoroutines {
		wg.Go(func() {
			base := g * keysPerGoroutine
			for i := range keysPerGoroutine {
				m.Delete(base + i)
			}
		})
	}
	wg.Wait()

	m.Range(func(k, v int) bool {
		m.Delete(k)
		return true
	})

	if got := m.Len(); got != 0 {
		t.Fatalf("after storing and deleting all keys, Len() = %d, want 0", got)
	}
}

// TestMap_MatchesDeepCopy runs randomized property tests to ensure the Map's concurrent behavior matches an inherently safe deep-copied Map.
func TestMap_MatchesDeepCopy(t *testing.T) {
	if err := quick.CheckEqual(applyMap, applyDeepCopyMap, nil); err != nil {
		t.Error(err)
	}
}

// TestMap_MatchesRWMutex runs randomized property tests to verify the Map aligns strictly with the behavior of a sync.RWMutex-guarded equivalent.
func TestMap_MatchesRWMutex(t *testing.T) {
	if err := quick.CheckEqual(applyMap, applyRWMutexMap, nil); err != nil {
		t.Error(err)
	}
}

// TestMap_RangeNestedCall tests the safety and stability of invoking nested Range callbacks traversing the map recursively.
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
