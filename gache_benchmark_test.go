package gache

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

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

var (
	ttl time.Duration = 50 * time.Millisecond

	smallData []keyValue
	bigData   []keyValue
)

func init() {
	flag.StringVar(&benchParallelismFlag, "benchparallelism", "", "comma-separated list of parallelism values for benchmarks (default: 100,1000,5000,10000)")

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

func benchmark(b *testing.B, data []keyValue,
	t time.Duration,
	set func(string, string, time.Duration),
	get func(string),
) {
	b.Helper()
	nprocs := runtime.GOMAXPROCS(0)
	for _, p := range parallelismValues {
		b.Run(fmt.Sprintf("P%d", p), func(b *testing.B) {
			mp := max(p/nprocs, 1)
			b.SetParallelism(mp)
			runBenchParallel(b, func(_ *testing.PB, _ int) {
				for _, kv := range data {
					set(kv.key, kv.value, t)
				}
				for _, kv := range data {
					get(kv.key)
				}
			})
		})
	}
}

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

// BenchmarkExt_DefaultMapBigDataNoTTL evaluates the throughput of the standard sync.RWMutex-backed map using large data payloads without any TTL expiration.
func BenchmarkExt_DefaultMapBigDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

// BenchmarkExt_DefaultMapSmallDataNoTTL evaluates the throughput of the standard sync.RWMutex-backed map using small data payloads without any TTL expiration.
func BenchmarkExt_DefaultMapSmallDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

// BenchmarkExt_SyncMapBigDataNoTTL measures the performance of the sync.Map implementation handling large byte slice payloads under concurrent access.
func BenchmarkExt_SyncMapBigDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

// BenchmarkExt_SyncMapSmallDataNoTTL measures the performance of the sync.Map implementation handling small payloads under concurrent access.
func BenchmarkExt_SyncMapSmallDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

// BenchmarkGache_BigDataNoTTL assesses gache's baseline performance when handling large datasets with TTL expiration disabled, isolating the core hashing and sharding overhead.
func BenchmarkGache_BigDataNoTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_BigDataWithTTL assesses gache's performance when handling large datasets with active TTL expiration, including timestamp tracking overhead.
func BenchmarkGache_BigDataWithTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, bigData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_SmallDataNoTTL assesses gache's efficiency handling small string key-value pairs without TTL tracking.
func BenchmarkGache_SmallDataNoTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_SmallDataWithTTL assesses gache's efficiency handling small string key-value pairs while tracking expiration times.
func BenchmarkGache_SmallDataWithTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, smallData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

// BenchmarkGache_ChangeOutAllInt evaluates the sustained throughput of gache when continuously replacing all integer keys, simulating high cache churn.
func BenchmarkGache_ChangeOutAllInt(b *testing.B) {
	gc := New[int64]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), i+1)
	})
}

// BenchmarkGache_DeleteExpired measures the throughput and lock contention of the periodic background sweep responsible for pruning expired cache entries.
func BenchmarkGache_DeleteExpired(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := New(
		WithDefaultExpiration[string](1 * time.Nanosecond),
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

// BenchmarkGache_GetInt tests the peak read-only throughput of gache for a single, highly-contended integer key.
func BenchmarkGache_GetInt(b *testing.B) {
	gc := New[string]().SetDefaultExpire(10 * time.Second)
	gc.Set("0", "0")
	runBenchLoop(b, func(_ int64) {
		gc.Get("0")
	})
}

// BenchmarkGache_HeavyMixedInt simulates a high-intensity workload with a mixture of reads and writes on integer keys to measure contention management.
func BenchmarkGache_HeavyMixedInt(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(_ *testing.PB, _ int) {
		for i := range 8192 {
			gc.Set(Int64Key(int64(i)), i+1)
			gc.Get(Int64Key(int64(i)))
		}
	})
}

// BenchmarkGache_HeavyReadInt evaluates the scalability of read operations across multiple goroutines accessing a pre-populated integer cache.
func BenchmarkGache_HeavyReadInt(b *testing.B) {
	gc := New[int64]().SetDefaultExpire(10 * time.Second)
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
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(_ *testing.PB, start int) {
		for i := range 8192 {
			gc.Set(Int64Key(int64(i+start)), Data1K)
		}
	})
}

// BenchmarkGache_HeavyWriteInt measures write performance under heavy concurrency for integer values.
func BenchmarkGache_HeavyWriteInt(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
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
	g := New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = g.Keys(ctx)
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
				g.loop(ctx, func(shardID int, k string, v *value[string]) bool {
					return true
				})
			}
		})
	}
}

// BenchmarkGache_Mixed_90Read_10Write represents a realistic application workload consisting of 90% read operations and 10% write operations.
func BenchmarkGache_Mixed_90Read_10Write(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
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
	gc := New[int]().SetDefaultExpire(10 * time.Second)
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
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), Data1K)
	})
}

// BenchmarkGache_Put1M benchmarks the ingestion speed of 1-megabyte data chunks, emphasizing memory allocation overhead.
func BenchmarkGache_Put1M(b *testing.B) {
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), Data1M)
	})
}

// BenchmarkGache_PutInt measures the baseline insertion speed for primitive integer types.
func BenchmarkGache_PutInt(b *testing.B) {
	gc := New[int64]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), i+1)
	})
}

// BenchmarkGache_PutTinyObject assesses insertion performance for small struct instances, analyzing interface wrapping overhead.
func BenchmarkGache_PutTinyObject(b *testing.B) {
	gc := New[dummyData]().SetDefaultExpire(10 * time.Second)
	runBenchLoop(b, func(i int64) {
		gc.Set(Int64Key(i), dummyData{})
	})
}

// BenchmarkGache_Range tests the traversal speed of the cache's public Range method under normal conditions.
func BenchmarkGache_Range(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := New(
		WithDefaultExpiration[string](NoTTL),
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

// BenchmarkGache_Read benchmarks the serialization or mass retrieval throughput for the entire cache contents.
func BenchmarkGache_Read(b *testing.B) {
	gc := New[int]()
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
		err := New[int]().Read(bytes.NewReader(data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGache_SetIfNotExists evaluates the cost of conditional insertions (CAS-like logic) under highly concurrent access.
func BenchmarkGache_SetIfNotExists(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	runBenchParallel(b, func(pb *testing.PB, i int) {
		for pb.Next() {
			gc.SetIfNotExists("constant_key", i)
		}
	})
}

// BenchmarkGache_ToMap measures the performance and memory footprint of cloning the entire cache into a standard map[string]interface{}.
func BenchmarkGache_ToMap(b *testing.B) {
	b.ReportAllocs()
	gc := New[int]()
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
	g := New[int]()
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
	g := New[int]()
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
	m := &Map[int, int]{}
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
	code := m.Run()
	os.Exit(code)
}

// BenchmarkGache_StringKeys_Short evaluates throughput for keys short enough to trigger the fast-path byte hasher.
func BenchmarkGache_StringKeys_Short(b *testing.B) {
	g := New[string]()
	key := "short"
	runBenchLoop(b, func(i int64) {
		g.Set(key, "val")
		g.Get(key)
	})
}

// BenchmarkGache_StringKeys_Long evaluates throughput for keys long enough to require the fallback xxh3 hashing algorithm.
func BenchmarkGache_StringKeys_Long(b *testing.B) {
	g := New[string]()
	key := strings.Repeat("long_key_string_", 10)
	runBenchLoop(b, func(i int64) {
		g.Set(key, "val")
		g.Get(key)
	})
}

// BenchmarkGache_HeavyContention deliberately induces maximum lock contention on a single key to test the shard's synchronization boundaries.
func BenchmarkGache_HeavyContention(b *testing.B) {
	g := New[string]()
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
