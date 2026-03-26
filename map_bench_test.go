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
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/kpango/gache/v2"
)

type bench struct {
	setup func(*testing.B, mapInterface)
	perG  func(b *testing.B, pb *testing.PB, i int, m mapInterface)
}

func benchMap(b *testing.B, bench bench) {
	b.Helper()
	for _, m := range [...]mapInterface{&DeepCopyMap{}, &RWMutexMap{}, &gache.Map[any, any]{}} {
		b.Run(fmt.Sprintf("%T", m), func(b *testing.B) {
			m = reflect.New(reflect.TypeOf(m).Elem()).Interface().(mapInterface)
			if bench.setup != nil {
				bench.setup(b, m)
			}

			b.ResetTimer()
			b.ReportAllocs()

			var i int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(atomic.AddInt64(&i, 1) - 1)
				bench.perG(b, pb, id*b.N, m)
			})
		})
	}
}

// BenchmarkMap_AdversarialAlloc evaluates map performance under a pattern designed to maximize allocation and garbage collection overhead.
func BenchmarkMap_AdversarialAlloc(b *testing.B) {
	benchMap(b, bench{
		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			var stores, loadsSinceStore int64
			for ; pb.Next(); i++ {
				m.Load(i)
				if loadsSinceStore++; loadsSinceStore > stores {
					m.LoadOrStore(i, stores)
					loadsSinceStore = 0
					stores++
				}
			}
		},
	})
}

// BenchmarkMap_AdversarialDelete stresses the map by continuously interleaving single-item deletions and insertions to force structural resizing.
func BenchmarkMap_AdversarialDelete(b *testing.B) {
	const mapSize = 1 << 10

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range mapSize {
				m.Store(i, i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Load(i)

				if i%mapSize == 0 {
					m.Range(func(k, _ any) bool {
						m.Delete(k)
						return false
					})
					m.Store(i, i)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndDeleteCollision measures the cost of contested CompareAndDelete operations where multiple threads target the same item.
func BenchmarkMap_CompareAndDeleteCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				if m.CompareAndDelete(0, 0) {
					m.Store(0, 0)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndDeleteMostlyHits assesses the throughput of CompareAndDelete operations under conditions where the target value generally matches.
func BenchmarkMap_CompareAndDeleteMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}

			for i := range hits {
				m.LoadOrStore(i, i)
			}
			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				if m.CompareAndDelete(v, v) {
					m.Store(v, v)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndDeleteMostlyMisses assesses the throughput of CompareAndDelete operations under conditions where the target value is usually incorrect.
func BenchmarkMap_CompareAndDeleteMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}
			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				if m.CompareAndDelete(v, v) {
					m.Store(v, v)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndSwapCollision evaluates the performance penalty of high-contention CompareAndSwap requests on a shared key.
func BenchmarkMap_CompareAndSwapCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for pb.Next() {
				if m.CompareAndSwap(0, 0, 42) {
					m.CompareAndSwap(0, 42, 0)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndSwapMostlyHits measures CAS operations in an environment tailored to produce a high success rate.
func BenchmarkMap_CompareAndSwapMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}

			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				m.CompareAndSwap(v, v, v)
			}
		},
	})
}

// BenchmarkMap_CompareAndSwapMostlyMisses measures CAS operations in an environment tailored to produce a high failure rate.
func BenchmarkMap_CompareAndSwapMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				v := i
				if i%(hits+misses) < hits {
					v = i % (hits + misses)
				}
				m.CompareAndSwap(v, v, v)
			}
		},
	})
}

// BenchmarkMap_CompareAndSwapNoExistingKey tests the speed of CompareAndSwap when attempting to mutate a key that has not yet been allocated.
func BenchmarkMap_CompareAndSwapNoExistingKey(b *testing.B) {
	benchMap(b, bench{
		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				if m.CompareAndSwap(i, 0, 0) {
					m.Delete(i)
				}
			}
		},
	})
}

// BenchmarkMap_CompareAndSwapValueNotEqual measures the map's rejection speed for CAS operations carrying mismatched comparison values.
func BenchmarkMap_CompareAndSwapValueNotEqual(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.Store(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.CompareAndSwap(0, 1, 2)
			}
		},
	})
}

// BenchmarkMap_DeleteCollision assesses the lock and coordination overhead when many threads attempt to delete the identical key simultaneously.
func BenchmarkMap_DeleteCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Delete(0)
			}
		},
	})
}

// BenchmarkMap_LoadAndDeleteBalanced evaluates the performance of LoadAndDelete using an even distribution of existing and non-existing keys.
func BenchmarkMap_LoadAndDeleteBalanced(b *testing.B) {
	const hits, misses = 128, 128

	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				j := i % (hits + misses)
				if j < hits {
					m.LoadAndDelete(j)
				} else {
					m.LoadAndDelete(i)
				}
			}
		},
	})
}

// BenchmarkMap_LoadAndDeleteCollision tests throughput when multiple goroutines attempt to LoadAndDelete the exact same key.
func BenchmarkMap_LoadAndDeleteCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				if _, loaded := m.LoadAndDelete(0); loaded {
					m.Store(0, 0)
				}
			}
		},
	})
}

// BenchmarkMap_LoadAndDeleteUnique measures the optimal throughput of LoadAndDelete when each thread targets independent keys.
func BenchmarkMap_LoadAndDeleteUnique(b *testing.B) {
	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.LoadAndDelete(i)
			}
		},
	})
}

// BenchmarkMap_LoadMostlyHits gauges basic read performance when keys are almost always found within the map.
func BenchmarkMap_LoadMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Load(i % (hits + misses))
			}
		},
	})
}

// BenchmarkMap_LoadMostlyMisses gauges basic read performance when keys are almost always absent from the map.
func BenchmarkMap_LoadMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Load(i % (hits + misses))
			}
		},
	})
}

// BenchmarkMap_LoadOrStoreBalanced tests LoadOrStore utilizing a mix of insertions that both succeed and fail due to pre-existing data.
func BenchmarkMap_LoadOrStoreBalanced(b *testing.B) {
	const hits, misses = 128, 128

	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				j := i % (hits + misses)
				if j < hits {
					if _, ok := m.LoadOrStore(j, i); !ok {
						b.Fatalf("unexpected miss for %v", j)
					}
				} else {
					if v, loaded := m.LoadOrStore(i, i); loaded {
						b.Fatalf("failed to store %v: existing value %v", i, v)
					}
				}
			}
		},
	})
}

// BenchmarkMap_LoadOrStoreCollision evaluates LoadOrStore behavior when a massive number of goroutines attempt to initialize the same key.
func BenchmarkMap_LoadOrStoreCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.LoadOrStore(0, 0)
			}
		},
	})
}

// BenchmarkMap_LoadOrStoreUnique measures the throughput of LoadOrStore when keys do not overlap across parallel threads.
func BenchmarkMap_LoadOrStoreUnique(b *testing.B) {
	benchMap(b, bench{
		setup: func(b *testing.B, m mapInterface) {
			if _, ok := m.(*DeepCopyMap); ok {
				b.Skip("DeepCopyMap has quadratic running time.")
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.LoadOrStore(i, i)
			}
		},
	})
}

// BenchmarkMap_Range evaluates the iteration speed of the map's internal items across multiple concurrent callers.
func BenchmarkMap_Range(b *testing.B) {
	const mapSize = 1 << 10

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range mapSize {
				m.Store(i, i)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Range(func(_, _ any) bool { return true })
			}
		},
	})
}

// BenchmarkMap_SwapCollision tests the performance degradation associated with many goroutines violently swapping the value of a single key.
func BenchmarkMap_SwapCollision(b *testing.B) {
	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			m.LoadOrStore(0, 0)
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				m.Swap(0, 0)
			}
		},
	})
}

// BenchmarkMap_SwapMostlyHits measures Swap performance when the targeted keys overwhelmingly exist in the map.
func BenchmarkMap_SwapMostlyHits(b *testing.B) {
	const hits, misses = 1023, 1

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				if i%(hits+misses) < hits {
					v := i % (hits + misses)
					m.Swap(v, v)
				} else {
					m.Swap(i, i)
					m.Delete(i)
				}
			}
		},
	})
}

// BenchmarkMap_SwapMostlyMisses measures Swap performance when the targeted keys largely do not exist, triggering continuous insertions.
func BenchmarkMap_SwapMostlyMisses(b *testing.B) {
	const hits, misses = 1, 1023

	benchMap(b, bench{
		setup: func(_ *testing.B, m mapInterface) {
			for i := range hits {
				m.LoadOrStore(i, i)
			}

			for i := range hits * 2 {
				m.Load(i % hits)
			}
		},

		perG: func(b *testing.B, pb *testing.PB, i int, m mapInterface) {
			for ; pb.Next(); i++ {
				if i%(hits+misses) < hits {
					v := i % (hits + misses)
					m.Swap(v, v)
				} else {
					m.Swap(i, i)
					m.Delete(i)
				}
			}
		},
	})
}
