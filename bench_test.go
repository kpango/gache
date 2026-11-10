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
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kpango/gache/v2"
)

// Black-box benchmarks. Benchmarks that need unexported internals live next to
// the white-box tests that own their subject: BenchmarkGache_Loop in
// gache_internal_test.go and BenchmarkGetShardID in hash_test.go.
//
// Existing Benchmark names and workloads are kept as they were (bench-compare
// history); BenchmarkGache_Delete, BenchmarkGache_Iterate and the
// BenchmarkCache_* matrix at the end are additions. Where allocs/op is below 1
// but B/op is not zero, allocs/op prints as 0 (integer truncation).

type DefaultMap struct {
	data map[any]any
	mu   sync.RWMutex
}

func NewDefault() *DefaultMap {
	return &DefaultMap{
		data: make(map[any]any),
	}
}

func (m *DefaultMap) Get(key any) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *DefaultMap) Set(key, val any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = val
}

var benchParallelismFlag string

var parallelismValues []int

type keyValue struct {
	key   string
	value string
}

var ttl time.Duration = 50 * time.Millisecond

func init() {
	flag.StringVar(&benchParallelismFlag, "benchparallelism", "", "comma-separated list of parallelism values for benchmarks (default: 100,1000,5000,10000)")
}

var (
	benchDataOnce sync.Once
	smallData     []keyValue
	bigData       []keyValue
)

// loadBenchData builds the big/small key-value sets on first use, so a plain
// `go test` does not pay for ~500 MiB of random strings. The draw order (big,
// then small, one shared source) is the same as it was at init time.
func loadBenchData() {
	benchDataOnce.Do(func() {
		var (
			bigDataLen     = 2 << 10
			bigDataCount   = 2 << 16
			smallDataLen   = 2 << 5
			smallDataCount = 2 << 3
		)
		bigData = make([]keyValue, 0, bigDataCount)
		for range bigDataCount {
			bigData = append(bigData, keyValue{
				key:   randStr(bigDataLen),
				value: randStr(bigDataLen),
			})
		}
		slices.SortFunc(bigData, func(a, b keyValue) int {
			return strings.Compare(a.key, b.key)
		})
		smallData = make([]keyValue, 0, smallDataCount)
		for range smallDataCount {
			smallData = append(smallData, keyValue{
				key:   randStr(smallDataLen),
				value: randStr(smallDataLen),
			})
		}
		slices.SortFunc(smallData, func(a, b keyValue) int {
			return strings.Compare(a.key, b.key)
		})
	})
}

var randSrc = rand.NewSource(42)

const (
	rs6Letters       = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	rs6LetterIdxBits = 6
	rs6LetterIdxMask = 1<<rs6LetterIdxBits - 1
	rs6LetterIdxMax  = 63 / rs6LetterIdxBits
)

func randStr(n int) string {
	b := make([]byte, n)
	cache, remain := randSrc.Int63(), rs6LetterIdxMax
	for i := n - 1; i >= 0; {
		if remain == 0 {
			cache, remain = randSrc.Int63(), rs6LetterIdxMax
		}
		idx := int(cache & rs6LetterIdxMask)
		if idx < len(rs6Letters) {
			b[i] = rs6Letters[idx]
			i--
		}
		cache >>= rs6LetterIdxBits
		remain--
	}
	return string(b)
}

func benchmark(b *testing.B, data func() []keyValue,
	t time.Duration,
	set func(string, string, time.Duration),
	get func(string),
) {
	b.Helper()
	loadBenchData()
	d := data()
	nprocs := runtime.GOMAXPROCS(0)
	for _, p := range parallelismValues {
		b.Run(fmt.Sprintf("P%d", p), func(b *testing.B) {
			mp := max(p/nprocs, 1)
			b.SetParallelism(mp)
			runBenchParallel(b, func(_ *testing.PB, _ int) {
				for _, kv := range d {
					set(kv.key, kv.value, t)
				}
				for _, kv := range d {
					get(kv.key)
				}
			})
		})
	}
}

func bigDataSet() []keyValue   { return bigData }
func smallDataSet() []keyValue { return smallData }

func runBenchLoop(b *testing.B, f func(i int64)) {
	b.Helper()
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	var i int64
	for b.Loop() {
		f(i)
		i++
	}
}

func runBenchParallel(b *testing.B, f func(pb *testing.PB, i int)) {
	b.Helper()
	runtime.GC()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			f(pb, i)
			i++
		}
	})
}

func setup() {
	debug.SetGCPercent(10)
}

func Int64Key(d int64) string {
	return strconv.FormatInt(d, 10)
}

func randomString(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.Intn(26) + 'a')
	}
	return b
}

var (
	Data1K = randomString(1024)
	Data1M = randomString(1048576)
)

type dummyData struct {
	Name    string
	Company string
	Skills  []string
	Age     int32
	Gender  int32
}

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

			var i atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				id := int(i.Add(1) - 1)
				bench.perG(b, pb, id*b.N, m)
			})
		})
	}
}

// BenchmarkExt_DefaultMapBigDataNoTTL evaluates the throughput of the standard sync.RWMutex-backed map using large data payloads without any TTL expiration.
func BenchmarkExt_DefaultMapBigDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, bigDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

// BenchmarkExt_DefaultMapSmallDataNoTTL evaluates the throughput of the standard sync.RWMutex-backed map using small data payloads without any TTL expiration.
func BenchmarkExt_DefaultMapSmallDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, smallDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

// BenchmarkExt_SyncMapBigDataNoTTL measures the performance of the sync.Map implementation handling large string payloads under concurrent access.
func BenchmarkExt_SyncMapBigDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, bigDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

// BenchmarkExt_SyncMapSmallDataNoTTL measures the performance of the sync.Map implementation handling small payloads under concurrent access.
func BenchmarkExt_SyncMapSmallDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, smallDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

// BenchmarkGache_BigDataNoTTL assesses gache's baseline performance when handling large datasets with TTL expiration disabled, isolating the core hashing and sharding overhead.
func BenchmarkGache_BigDataNoTTL(b *testing.B) {
	g := gache.New(
		gache.WithMaxKeyLength[string](0),
		gache.WithDefaultExpiration[string](gache.NoTTL),
	)
	benchmark(b, bigDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_BigDataWithTTL assesses gache's performance when handling large datasets with active TTL expiration, including timestamp tracking overhead.
func BenchmarkGache_BigDataWithTTL(b *testing.B) {
	g := gache.New(
		gache.WithMaxKeyLength[string](0),
		gache.WithDefaultExpiration[string](ttl),
	)
	benchmark(b, bigDataSet, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_SmallDataNoTTL assesses gache's efficiency handling small string key-value pairs without TTL tracking.
func BenchmarkGache_SmallDataNoTTL(b *testing.B) {
	g := gache.New(
		gache.WithMaxKeyLength[string](0),
		gache.WithDefaultExpiration[string](gache.NoTTL),
	)
	benchmark(b, smallDataSet, gache.NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_SmallDataWithTTL assesses gache's efficiency handling small string key-value pairs while tracking expiration times.
func BenchmarkGache_SmallDataWithTTL(b *testing.B) {
	g := gache.New(
		gache.WithMaxKeyLength[string](0),
		gache.WithDefaultExpiration[string](ttl),
	)
	benchmark(b, smallDataSet, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_ChangeOutAllInt evaluates the sustained throughput of gache when continuously replacing all integer keys, simulating high cache churn.
func BenchmarkGache_ChangeOutAllInt(b *testing.B) {
	gc := gache.New[int64]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), i+1)
	})
}

// BenchmarkGache_DeleteExpired measures the throughput and lock contention of the periodic background sweep responsible for pruning expired cache entries: it calls DeleteExpired directly (no background daemon) and refills the keys inside the loop, so the timed work includes the refill.
func BenchmarkGache_DeleteExpired(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := gache.New(
		gache.WithDefaultExpiration[string](1 * time.Nanosecond),
	)
	// Pre-fill with keys that will expire immediately.
	for i := range numKeys {
		g.SetWithExpire(strconv.Itoa(i), "v", 1*time.Nanosecond)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		g.DeleteExpired(ctx)
		// Refill so next iteration has something to delete.
		for i := range numKeys {
			g.SetWithExpire(strconv.Itoa(i), "v", 1*time.Nanosecond)
		}
	}
}

// BenchmarkGache_GetInt tests the peak read-only throughput of gache for a single string key from one goroutine (runBenchLoop, no contention).
func BenchmarkGache_GetInt(b *testing.B) {
	gc := gache.New[string]().SetDefaultExpire(10 * time.Second)
	gc.Set("0", "0")
	runBenchLoop(b, func(_ int64) {
		gc.Get("0")
	})
}

// BenchmarkGache_HeavyMixedInt simulates a high-intensity workload with a mixture of reads and writes on integer keys to measure contention management.
func BenchmarkGache_HeavyMixedInt(b *testing.B) {
	gc := gache.New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(_ *testing.PB, _ int) {
		for i := range 8192 {
			gc.Set(Int64Key(int64(i)), i+1)
			gc.Get(Int64Key(int64(i)))
		}
	})
}

// BenchmarkGache_HeavyReadInt evaluates the scalability of read operations across multiple goroutines accessing a pre-populated integer cache.
func BenchmarkGache_HeavyReadInt(b *testing.B) {
	gc := gache.New[int64]().SetDefaultExpire(10 * time.Second)
	for i := range int64(1024) {
		gc.Set(Int64Key(i), i+1)
	}
	runBenchParallel(b, func(_ *testing.PB, _ int) {
		for i := range 1024 {
			gc.Get(Int64Key(int64(i)))
		}
	})
}

// BenchmarkGache_HeavyWrite1K measures write performance under heavy concurrency for 1-kilobyte payload sizes.
func BenchmarkGache_HeavyWrite1K(b *testing.B) {
	gc := gache.New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(_ *testing.PB, start int) {
		for i := range 8192 {
			gc.Set(Int64Key(int64(i+start)), Data1K)
		}
	})
}

// BenchmarkGache_HeavyWriteInt measures write performance under heavy concurrency for integer values.
func BenchmarkGache_HeavyWriteInt(b *testing.B) {
	gc := gache.New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(_ *testing.PB, start int) {
		for i := range 8192 {
			gc.Set(Int64Key(int64(i+start)), i+1)
		}
	})
}

// BenchmarkGache_Keys measures the overhead of allocating and populating a slice containing all current keys in the cache.
func BenchmarkGache_Keys(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	g := gache.New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = g.Keys(ctx)
	}
}

// BenchmarkGache_Mixed_90Read_10Write represents a realistic application workload consisting of 90% read operations and 10% write operations.
func BenchmarkGache_Mixed_90Read_10Write(b *testing.B) {
	gc := gache.New[int]().SetDefaultExpire(10 * time.Second)
	for i := range 1000 {
		gc.Set(Int64Key(int64(i)), i)
	}
	runBenchParallel(b, func(pb *testing.PB, i int) {
		r := i
		for pb.Next() {
			r++
			key := Int64Key(int64(r % 1000))
			if r%10 == 0 {
				gc.Set(key, r)
			} else {
				gc.Get(key)
			}
		}
	})
}

// BenchmarkGache_Pop tests the performance of concurrent extraction operations (retrieval and deletion in a single step).
func BenchmarkGache_Pop(b *testing.B) {
	gc := gache.New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(pb *testing.PB, i int) {
		r := i
		for pb.Next() {
			r++
			key := Int64Key(int64(r % 10000))
			gc.Set(key, r)
			gc.Pop(key)
		}
	})
}

// BenchmarkGache_Put1K benchmarks the ingestion speed of 1-kilobyte data chunks into the cache.
func BenchmarkGache_Put1K(b *testing.B) {
	gc := gache.New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), Data1K)
	})
}

// BenchmarkGache_Put1M benchmarks the ingestion speed of 1-megabyte data chunks, emphasizing memory allocation overhead.
func BenchmarkGache_Put1M(b *testing.B) {
	gc := gache.New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), Data1M)
	})
}

// BenchmarkGache_PutInt measures the baseline insertion speed for primitive integer types.
func BenchmarkGache_PutInt(b *testing.B) {
	gc := gache.New[int64]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), i+1)
	})
}

// BenchmarkGache_PutTinyObject assesses insertion performance for small struct instances, analyzing interface wrapping overhead.
func BenchmarkGache_PutTinyObject(b *testing.B) {
	gc := gache.New[dummyData]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), dummyData{})
	})
}

// BenchmarkGache_Range tests the traversal speed of the cache's public Range method under normal conditions.
func BenchmarkGache_Range(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := gache.New(
		gache.WithDefaultExpiration[string](gache.NoTTL),
	)
	for i := range numKeys {
		g.Set(strconv.Itoa(i), "v")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		g.Range(ctx, func(_ string, _ string, _ int64) bool { return true })
	}
}

// BenchmarkGache_Read benchmarks the deserialization (Read) throughput of a 10M-entry cache dump into a fresh cache.
func BenchmarkGache_Read(b *testing.B) {
	gc := gache.New[int]()
	for i := range 10000000 {
		gc.Set(fmt.Sprintf("key-%d", i), i)
	}

	var buf bytes.Buffer
	err := gc.Write(context.Background(), &buf)
	if err != nil {
		b.Fatal(err)
	}
	gc.Stop()
	data := buf.Bytes()

	b.ResetTimer()
	for b.Loop() {
		err := gache.New[int]().Read(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGache_SetIfNotExists evaluates the cost of conditional insertions (CAS-like logic) under highly concurrent access.
func BenchmarkGache_SetIfNotExists(b *testing.B) {
	gc := gache.New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(pb *testing.PB, i int) {
		for pb.Next() {
			gc.SetIfNotExists("constant_key", i)
		}
	})
}

// BenchmarkGache_ToMap measures the performance and memory footprint of cloning the entire cache into a *sync.Map (ToMap).
func BenchmarkGache_ToMap(b *testing.B) {
	b.ReportAllocs()
	gc := gache.New[int]()
	for i := range 100000 {
		gc.Set(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = gc.ToMap(context.Background())
	}
}

// BenchmarkGache_ToRawMap measures the performance of exporting cache shards into a native map representation.
func BenchmarkGache_ToRawMap(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	g := gache.New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = g.ToRawMap(ctx)
	}
}

// BenchmarkGache_Values measures the time required to extract all cache values into a newly allocated slice.
func BenchmarkGache_Values(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	g := gache.New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = g.Values(ctx)
	}
}

// BenchmarkMap_Advanced_ConcurrentExpungeAndCAS stresses the map's ability to handle concurrent item expungement alongside CompareAndSwap operations.
func BenchmarkMap_Advanced_ConcurrentExpungeAndCAS(b *testing.B) {
	m := &gache.Map[int, int]{}
	runBenchParallel(b, func(pb *testing.PB, i int) {
		r := i
		for pb.Next() {
			r++
			key := r % 100
			m.Store(key, i)
			m.CompareAndSwap(key, i, i+1)
			m.LoadAndDelete(key)
		}
	})
}

// BenchmarkGache_StringKeys_Short evaluates throughput for keys short enough to trigger the fast-path byte hasher.
func BenchmarkGache_StringKeys_Short(b *testing.B) {
	g := gache.New[string]()
	key := "short"
	runBenchLoop(b, func(i int64) {
		g.Set(key, "val")
		g.Get(key)
	})
}

// BenchmarkGache_StringKeys_Long evaluates throughput for keys long enough to take the sampled hash (xxh3, or maphash on arm64) instead of the short-key path.
func BenchmarkGache_StringKeys_Long(b *testing.B) {
	g := gache.New[string]()
	key := strings.Repeat("long_key_string_", 10)
	runBenchLoop(b, func(i int64) {
		g.Set(key, "val")
		g.Get(key)
	})
}

// BenchmarkGache_HeavyContention deliberately induces maximum lock contention on a single key to test the shard's synchronization boundaries.
// Note: f drains pb in its own loop, so runBenchParallel calls it once per
// goroutine with i == 0: i%100 == 0 always holds and every goroutine Sets on
// every iteration. Kept as is for bench-compare history.
func BenchmarkGache_HeavyContention(b *testing.B) {
	g := gache.New[string]()
	g.Set("hot_key", "value")
	runBenchParallel(b, func(pb *testing.PB, i int) {
		for pb.Next() {
			g.Get("hot_key")
			if i%100 == 0 {
				g.Set("hot_key", "value")
			}
		}
	})
}

// BenchmarkEmpirical_M2_New measures standard heap allocations and latency of gache.New[int]().
// The name is kept for bench-compare history.
func BenchmarkEmpirical_M2_New(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gache.New[int]()
	}
}

const refreshKeyCount = 1024

func newRefreshBench(b *testing.B) (gache.Gache[int], []string) {
	b.Helper()
	g := gache.New[int]().SetDefaultExpire(time.Hour)
	keys := make([]string, refreshKeyCount)
	for i := range keys {
		keys[i] = Int64Key(int64(i))
		g.Set(keys[i], i)
	}
	return g, keys
}

// BenchmarkGache_ExtendExpire measures concurrent TTL extension spread over many live keys.
func BenchmarkGache_ExtendExpire(b *testing.B) {
	g, keys := newRefreshBench(b)
	runBenchParallel(b, func(_ *testing.PB, i int) {
		g.ExtendExpire(keys[i%refreshKeyCount], time.Nanosecond)
	})
}

// BenchmarkGache_ExtendExpire_HotKey measures TTL extension contention on a single key.
func BenchmarkGache_ExtendExpire_HotKey(b *testing.B) {
	g, keys := newRefreshBench(b)
	runBenchParallel(b, func(_ *testing.PB, _ int) {
		g.ExtendExpire(keys[0], time.Nanosecond)
	})
}

// BenchmarkGache_GetRefreshWithDur measures concurrent sliding-TTL reads spread over many live keys.
func BenchmarkGache_GetRefreshWithDur(b *testing.B) {
	g, keys := newRefreshBench(b)
	runBenchParallel(b, func(_ *testing.PB, i int) {
		g.GetRefreshWithDur(keys[i%refreshKeyCount], time.Hour)
	})
}

// BenchmarkGache_GetRefreshWithDur_HotKey measures sliding-TTL read contention on a single key.
func BenchmarkGache_GetRefreshWithDur_HotKey(b *testing.B) {
	g, keys := newRefreshBench(b)
	runBenchParallel(b, func(_ *testing.PB, _ int) {
		g.GetRefreshWithDur(keys[0], time.Hour)
	})
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

// BenchmarkMap_Size measures the execution time of retrieving the total number of items stored in the concurrent map.
func BenchmarkMap_Size(b *testing.B) {
	m := &gache.Map[int, int]{}
	for i := range 10000 {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// BenchmarkMap_SizeOnlyDirty evaluates size calculation speed when elements reside exclusively in the unpromoted dirty sub-map.
func BenchmarkMap_SizeOnlyDirty(b *testing.B) {
	m := &gache.Map[int, int]{}
	m.Store(0, 0)
	for i := 1; i < 10000; i++ {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// BenchmarkMap_Size_Items_10 benchmarks the latency of calling Size on a map populated with exactly 10 elements.
func BenchmarkMap_Size_Items_10(b *testing.B) {
	m := &gache.Map[int, int]{}
	for i := range 10 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// BenchmarkMap_Size_Items_100 benchmarks the latency of calling Size on a map populated with exactly 100 elements.
func BenchmarkMap_Size_Items_100(b *testing.B) {
	m := &gache.Map[int, int]{}
	for i := range 100 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// BenchmarkMap_Size_Items_1000 benchmarks the latency of calling Size on a map populated with exactly 1,000 elements.
func BenchmarkMap_Size_Items_1000(b *testing.B) {
	m := &gache.Map[int, int]{}
	for i := range 1000 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// BenchmarkMap_Size_Items_10000 benchmarks the latency of calling Size on a map populated with exactly 10,000 elements.
func BenchmarkMap_Size_Items_10000(b *testing.B) {
	m := &gache.Map[int, int]{}
	for i := range 10000 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

// TestMain establishes the standard testing environment, configuring benchmark parallelism levels and initializing shared memory.
func TestMain(m *testing.M) {
	flag.Parse()
	if benchParallelismFlag != "" {
		for s := range strings.SplitSeq(benchParallelismFlag, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err == nil && v > 0 {
				parallelismValues = append(parallelismValues, v)
			}
		}
	}
	if len(parallelismValues) == 0 {
		parallelismValues = []int{100, 1000, 5000, 10000}
	}
	setup()
	// Benchmark runs keep the ~500 MiB live heap the old init-time data gave
	// (with GCPercent(10) below it sets the GC goal); plain `go test` stays light.
	if f := flag.Lookup("test.bench"); f != nil && f.Value.String() != "" {
		loadBenchData()
	}
	code := m.Run()
	os.Exit(code)
}

// ---- gap fillers (new names) ----

var iterSink atomic.Int64

// BenchmarkGache_Delete measures Delete: Live deletes live keys (each goroutine
// owns a block of keys and re-Sets the block when it has deleted all of it, so
// the timed loop includes one Set per Delete), Absent deletes keys that are not
// there.
func BenchmarkGache_Delete(b *testing.B) {
	const block = 4096
	keys := matrixKeyTable(block * 2)
	b.Run("Live", func(b *testing.B) {
		g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))
		b.Cleanup(g.Stop)
		// RunParallel starts exactly GOMAXPROCS goroutines. Goroutines must not
		// share keys, or a Delete finds another one's re-Set; the blocks and the
		// initial fill are built before the timer starts.
		procs := runtime.GOMAXPROCS(0)
		blocks := make([][]string, procs)
		for p := range blocks {
			pre := "g" + strconv.Itoa(p) + "-"
			blocks[p] = make([]string, block)
			for i := range blocks[p] {
				blocks[p][i] = pre + keys[i]
				g.Set(blocks[p][i], 1)
			}
		}
		var id atomic.Int64
		runtime.GC()
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			mine := blocks[int(id.Add(1)-1)%procs]
			var i int
			for pb.Next() {
				if _, ok := g.Delete(mine[i]); !ok {
					iterSink.Add(1)
				}
				if i++; i == block {
					i = 0
					for _, k := range mine {
						g.Set(k, 1)
					}
				}
			}
		})
	})
	b.Run("Absent", func(b *testing.B) {
		g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))
		b.Cleanup(g.Stop)
		for _, k := range keys[:block] {
			g.Set(k, 1)
		}
		absent := keys[block:]
		runtime.GC()
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			var i int
			for pb.Next() {
				g.Delete(absent[i%block])
				i++
			}
		})
	})
}

// BenchmarkGache_Iterate measures the whole-cache readers (Keys, Values, ToMap,
// ToRawMap, Range) at several sizes. The existing Gache_Keys/Values/ToMap/
// ToRawMap/Range benchmarks keep their single fixed sizes.
func BenchmarkGache_Iterate(b *testing.B) {
	ctx := context.Background()
	ops := []struct {
		run  func(g gache.Gache[int])
		name string
	}{
		{name: "Keys", run: func(g gache.Gache[int]) { iterSink.Add(int64(len(g.Keys(ctx)))) }},
		{name: "Values", run: func(g gache.Gache[int]) { iterSink.Add(int64(len(g.Values(ctx)))) }},
		{name: "ToMap", run: func(g gache.Gache[int]) {
			if g.ToMap(ctx) == nil {
				iterSink.Add(1)
			}
		}},
		{name: "ToRawMap", run: func(g gache.Gache[int]) { iterSink.Add(int64(len(g.ToRawMap(ctx)))) }},
		{name: "Range", run: func(g gache.Gache[int]) {
			// f writes nothing shared: gache.Range calls it from several fan-out
			// workers, and a shared counter would add contention to the measurement.
			g.Range(ctx, func(string, int, int64) bool { return true })
			iterSink.Add(1)
		}},
	}
	for _, op := range ops {
		for _, size := range []int{100, 10_000, 100_000} {
			b.Run(fmt.Sprintf("%s/%d", op.name, size), func(b *testing.B) {
				keys := matrixKeyTable(size)
				g := gache.New[int](gache.WithDefaultExpiration[int](gache.NoTTL))
				b.Cleanup(g.Stop)
				for i, k := range keys[:size] {
					g.Set(k, i)
				}
				runtime.GC()
				b.ReportAllocs()
				for b.Loop() {
					op.run(g)
				}
			})
		}
	}
}

// ---- cache matrix: gache / gache.Map / sync.Map / RWMutex map ----
//
// BenchmarkCache_Matrix/<impl>/<workload>, impl in {gache, gacheMap, syncMap,
// lockMap}. Every workload is a pre-generated ring of operations per goroutine
// (fixed-seed rand, so the timed loop has no rand, strconv or key allocation)
// replayed through one small adapter interface, so each implementation runs the
// identical op stream. Values are int unless the workload says otherwise.
// Like every benchmark here it runs under the package TestMain's
// GCPercent(10); a runtime.GC() before the timer keeps setup garbage out of the
// timing. loadorstore/* only hit: every key is pre-populated.

type cacheOpKind uint8

const (
	mxGet cacheOpKind = iota
	mxSet
	mxDelete
	mxLoadOrStore
	mxSetTTL
	mxRange
	mxLen
)

type cacheOp struct {
	key  uint32
	kind cacheOpKind
}

const mxRingSize = 1 << 13

// matrixCache is the surface every implementation is driven through.
type matrixCache[V any] interface {
	Get(key string) (V, bool)
	Set(key string, v V)
	Delete(key string)
	// LoadOrStore stores v if key is absent; the result is ignored (gache has
	// SetIfNotExists, which returns nothing).
	LoadOrStore(key string, v V)
	Range(f func(key string, v V) bool)
	Len() int
	Close()
}

// ttlSetter is implemented by gache only (Map and sync.Map have no expiry).
type ttlSetter[V any] interface {
	SetWithTTL(key string, v V, d time.Duration)
}

type gacheCache[V any] struct{ g gache.Gache[V] }

func (c gacheCache[V]) Get(k string) (V, bool)    { return c.g.Get(k) }
func (c gacheCache[V]) Set(k string, v V)         { c.g.Set(k, v) }
func (c gacheCache[V]) Delete(k string)           { c.g.Delete(k) }
func (c gacheCache[V]) LoadOrStore(k string, v V) { c.g.SetIfNotExists(k, v) }
func (c gacheCache[V]) Len() int                  { return c.g.Len() }
func (c gacheCache[V]) Close()                    { c.g.Stop() }
func (c gacheCache[V]) SetWithTTL(k string, v V, d time.Duration) {
	c.g.SetWithExpire(k, v, d)
}

func (c gacheCache[V]) Range(f func(string, V) bool) {
	c.g.Range(context.Background(), func(k string, v V, _ int64) bool { return f(k, v) })
}

type gacheMapCache[V any] struct{ m gache.Map[string, V] }

func (c *gacheMapCache[V]) Get(k string) (V, bool)       { return c.m.Load(k) }
func (c *gacheMapCache[V]) Set(k string, v V)            { c.m.Store(k, v) }
func (c *gacheMapCache[V]) Delete(k string)              { c.m.Delete(k) }
func (c *gacheMapCache[V]) LoadOrStore(k string, v V)    { c.m.LoadOrStore(k, v) }
func (c *gacheMapCache[V]) Range(f func(string, V) bool) { c.m.Range(f) }
func (c *gacheMapCache[V]) Len() int                     { return c.m.Len() }
func (c *gacheMapCache[V]) Close()                       {}

type syncMapCache[V any] struct{ m sync.Map }

func (c *syncMapCache[V]) Get(k string) (V, bool) {
	v, ok := c.m.Load(k)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}
func (c *syncMapCache[V]) Set(k string, v V)         { c.m.Store(k, v) }
func (c *syncMapCache[V]) Delete(k string)           { c.m.Delete(k) }
func (c *syncMapCache[V]) LoadOrStore(k string, v V) { c.m.LoadOrStore(k, v) }
func (c *syncMapCache[V]) Close()                    {}
func (c *syncMapCache[V]) Range(f func(string, V) bool) {
	c.m.Range(func(k, v any) bool { return f(k.(string), v.(V)) })
}

// Len counts by ranging: sync.Map has no O(1) length.
func (c *syncMapCache[V]) Len() int {
	var n int
	c.m.Range(func(_, _ any) bool { n++; return true })
	return n
}

type lockMapCache[V any] struct {
	m  map[string]V
	mu sync.RWMutex
}

func (c *lockMapCache[V]) Get(k string) (V, bool) {
	c.mu.RLock()
	v, ok := c.m[k]
	c.mu.RUnlock()
	return v, ok
}

func (c *lockMapCache[V]) Set(k string, v V) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

func (c *lockMapCache[V]) Delete(k string) {
	c.mu.Lock()
	delete(c.m, k)
	c.mu.Unlock()
}

func (c *lockMapCache[V]) LoadOrStore(k string, v V) {
	c.mu.RLock()
	_, ok := c.m[k]
	c.mu.RUnlock()
	if ok {
		return
	}
	c.mu.Lock()
	if _, ok := c.m[k]; !ok {
		c.m[k] = v
	}
	c.mu.Unlock()
}

func (c *lockMapCache[V]) Range(f func(string, V) bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.m {
		if !f(k, v) {
			return
		}
	}
}

func (c *lockMapCache[V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

func (c *lockMapCache[V]) Close() {}

var matrixImpls = []string{"gache", "gacheMap", "syncMap", "lockMap"}

// newMatrixCache builds an empty cache; daemon starts gache's expiry daemon
// (10ms sweep) and is ignored by the others.
func newMatrixCache[V any](impl string, daemon bool) matrixCache[V] {
	switch impl {
	case "gache":
		g := gache.New[V](gache.WithDefaultExpiration[V](gache.NoTTL))
		if daemon {
			g.StartExpired(context.Background(), 10*time.Millisecond)
		}
		return gacheCache[V]{g: g}
	case "gacheMap":
		return &gacheMapCache[V]{}
	case "syncMap":
		return &syncMapCache[V]{}
	case "lockMap":
		return &lockMapCache[V]{m: make(map[string]V)}
	}
	panic("unknown matrix impl " + impl)
}

var (
	matrixKeyMu     sync.Mutex
	matrixKeyTables = map[int][]string{}
)

// matrixKeyTable returns 2n keys: [0,n) are the keys the workload populates,
// [n,2n) are guaranteed absent. Built once per n.
func matrixKeyTable(n int) []string {
	matrixKeyMu.Lock()
	defer matrixKeyMu.Unlock()
	if t, ok := matrixKeyTables[n]; ok {
		return t
	}
	t := make([]string, 2*n)
	for i := range n {
		t[i] = "key-" + strconv.Itoa(i)
		t[n+i] = "absent-" + strconv.Itoa(i)
	}
	matrixKeyTables[n] = t
	return t
}

// matrixWork describes one workload: how many keys are pre-populated and how
// each goroutine's op ring is filled.
type matrixWork struct {
	fill      func(rng *rand.Rand, ring []cacheOp, n int)
	name      string
	skip      string // impl this workload does not apply to
	keys      int
	ttl       time.Duration // > 0: mxSetTTL uses it and gache's expiry daemon runs
	gacheOnly bool
}

func fillRW(readPct int) func(*rand.Rand, []cacheOp, int) {
	return func(rng *rand.Rand, ring []cacheOp, n int) {
		for i := range ring {
			k := cacheOpKind(mxSet)
			if rng.Intn(100) < readPct {
				k = mxGet
			}
			ring[i] = cacheOp{key: uint32(rng.Intn(n)), kind: k}
		}
	}
}

// fillUniform fills with one kind over keys [off, off+n).
func fillUniform(kind cacheOpKind, absent bool) func(*rand.Rand, []cacheOp, int) {
	return func(rng *rand.Rand, ring []cacheOp, n int) {
		off := 0
		if absent {
			off = n
		}
		for i := range ring {
			ring[i] = cacheOp{key: uint32(off + rng.Intn(n)), kind: kind}
		}
	}
}

func fillZipf(readPct int) func(*rand.Rand, []cacheOp, int) {
	return func(rng *rand.Rand, ring []cacheOp, n int) {
		z := rand.NewZipf(rng, 1.1, 1, uint64(n-1))
		for i := range ring {
			k := cacheOpKind(mxSet)
			if rng.Intn(100) < readPct {
				k = mxGet
			}
			ring[i] = cacheOp{key: uint32(z.Uint64()), kind: k}
		}
	}
}

// fillChurn emits Delete(k), Set(k) pairs.
func fillChurn(rng *rand.Rand, ring []cacheOp, n int) {
	for i := 0; i+1 < len(ring); i += 2 {
		k := uint32(rng.Intn(n))
		ring[i] = cacheOp{key: k, kind: mxDelete}
		ring[i+1] = cacheOp{key: k, kind: mxSet}
	}
}

// fillTTL emits short-TTL Sets and Gets half and half.
func fillTTL(rng *rand.Rand, ring []cacheOp, n int) {
	for i := range ring {
		k := cacheOpKind(mxGet)
		if rng.Intn(2) == 0 {
			k = mxSetTTL
		}
		ring[i] = cacheOp{key: uint32(rng.Intn(n)), kind: k}
	}
}

var matrixSink atomic.Int64

// runMatrixWork runs one workload on one implementation under b.RunParallel.
func runMatrixWork[V any](b *testing.B, impl string, val V, w matrixWork) {
	if w.gacheOnly && impl != "gache" || w.skip == impl {
		return
	}
	b.Run(w.name, func(b *testing.B) {
		tab := matrixKeyTable(w.keys)
		c := newMatrixCache[V](impl, w.ttl > 0)
		b.Cleanup(c.Close)
		for _, k := range tab[:w.keys] {
			c.Set(k, val)
		}
		ts, hasTTL := c.(ttlSetter[V])
		if w.ttl > 0 && !hasTTL {
			b.Fatalf("%s cannot set a TTL", impl)
		}
		// RunParallel starts exactly GOMAXPROCS goroutines (parallelism 1).
		procs := runtime.GOMAXPROCS(0)
		rings := make([][]cacheOp, procs)
		for g := range rings {
			rings[g] = make([]cacheOp, mxRingSize)
			w.fill(rand.New(rand.NewSource(int64(g)+1)), rings[g], w.keys)
		}
		var id, gets, hits atomic.Int64
		b.ReportAllocs()
		runtime.GC()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			ring := rings[int(id.Add(1)-1)%procs]
			var i int
			var ng, nh, side int64
			// gache.Range calls rangeFn from several fan-out workers, so it must not
			// write shared state; the Range calls are counted in side instead.
			rangeFn := func(string, V) bool { return true }
			for pb.Next() {
				op := ring[i&(mxRingSize-1)]
				i++
				switch op.kind {
				case mxGet:
					ng++
					if _, ok := c.Get(tab[op.key]); ok {
						nh++
					}
				case mxSet:
					c.Set(tab[op.key], val)
				case mxDelete:
					c.Delete(tab[op.key])
				case mxLoadOrStore:
					c.LoadOrStore(tab[op.key], val)
				case mxSetTTL:
					ts.SetWithTTL(tab[op.key], val, w.ttl)
				case mxRange:
					side++
					c.Range(rangeFn)
				case mxLen:
					side += int64(c.Len())
				}
			}
			gets.Add(ng)
			hits.Add(nh)
			matrixSink.Add(side)
		})
		if g := gets.Load(); g > 0 {
			b.ReportMetric(float64(hits.Load())/float64(g)*100, "hit%")
		}
	})
}

// BenchmarkCache_Matrix compares gache, gache.Map, sync.Map and an RWMutex map
// on the same op streams: BenchmarkCache_Matrix/<impl>/<workload>. It has 80
// sub-benchmarks (21 workloads x 4 impls, minus len/10k for sync.Map and minus
// the gache-only TTL workload for the other three impls); select a slice with -bench, e.g.
// 'Cache_Matrix/.*/rw/' or 'Cache_Matrix/gache/'.
func BenchmarkCache_Matrix(b *testing.B) {
	var works []matrixWork
	for _, n := range []struct {
		name string
		keys int
	}{{"1k", 1_000}, {"100k", 100_000}} {
		for _, r := range []int{100, 90, 50, 10} {
			works = append(works, matrixWork{
				name: fmt.Sprintf("rw/read%d/keys%s", r, n.name),
				keys: n.keys,
				fill: fillRW(r),
			})
		}
	}
	works = append(works,
		matrixWork{name: "get/hit", keys: 10_000, fill: fillUniform(mxGet, false)},
		matrixWork{name: "get/miss", keys: 10_000, fill: fillUniform(mxGet, true)},
		matrixWork{name: "zipf/read90", keys: 100_000, fill: fillZipf(90)},
		matrixWork{name: "churn/delete_set", keys: 10_000, fill: fillChurn},
		matrixWork{name: "loadorstore/hit_hot16", keys: 16, fill: fillUniform(mxLoadOrStore, false)},
		matrixWork{name: "loadorstore/hit_keys100k", keys: 100_000, fill: fillUniform(mxLoadOrStore, false)},
		matrixWork{name: "range/10k", keys: 10_000, fill: fillUniform(mxRange, false)},
		// sync.Map has no Len (counting means a full Range), so it is not compared here.
		matrixWork{name: "len/10k", keys: 10_000, fill: fillUniform(mxLen, false), skip: "syncMap"},
		matrixWork{name: "ttl/set2ms_get", keys: 10_000, fill: fillTTL, ttl: 2 * time.Millisecond, gacheOnly: true},
	)
	sizes := []struct {
		run  func(b *testing.B, impl string, w matrixWork)
		name string
	}{
		{func(b *testing.B, impl string, w matrixWork) { runMatrixWork(b, impl, 0, w) }, "8B"},
		{func(b *testing.B, impl string, w matrixWork) { runMatrixWork(b, impl, [64]byte{1}, w) }, "64B"},
		{func(b *testing.B, impl string, w matrixWork) { runMatrixWork(b, impl, [1024]byte{1}, w) }, "1KB"},
		{func(b *testing.B, impl string, w matrixWork) { runMatrixWork(b, impl, [16384]byte{1}, w) }, "16KB"},
	}
	for _, impl := range matrixImpls {
		b.Run(impl, func(b *testing.B) {
			for _, w := range works {
				runMatrixWork(b, impl, 0, w)
			}
			for _, s := range sizes {
				s.run(b, impl, matrixWork{name: "valsize/" + s.name, keys: 1_000, fill: fillRW(90)})
			}
		})
	}
}
