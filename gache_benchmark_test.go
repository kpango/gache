package gache

import (
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

// benchParallelismFlag holds the raw flag value for -benchparallelism.
var benchParallelismFlag string

// parallelismValues is the set of parallelism levels used by all benchmarks.
// It is populated from -benchparallelism (comma-separated integers) in
// TestMain; see TestMain for the current default values.
var parallelismValues []int

// keyValue holds a pre-computed key-value pair for deterministic benchmark iteration.
type keyValue struct {
	key   string
	value string
}

var (
	ttl time.Duration = 50 * time.Millisecond

	// Pre-computed slices for deterministic iteration order in benchmarks.
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

// benchmark runs a mixed set-and-get workload benchmark for each configured
// parallelism value, emitting sub-benchmarks named "P<n>".
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
			b.ReportAllocs()
			runtime.GC()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					for _, kv := range data {
						set(kv.key, kv.value, t)
					}
					for _, kv := range data {
						get(kv.key)
					}
				}
			})
		})
	}
}

func BenchmarkDefaultMapSmallDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

func BenchmarkDefaultMapBigDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

func BenchmarkSyncMapSmallDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

func BenchmarkSyncMapBigDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

func BenchmarkGacheSmallDataNoTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheSmallDataWithTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, smallData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheBigDataNoTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheBigDataWithTTL(b *testing.B) {
	g := New(
		WithMaxKeyLength[string](0),
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, bigData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

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
	shutdown()
	os.Exit(code)
}

func setup() {
	debug.SetGCPercent(10)
}

func shutdown() {
	PrintGCPause()
	PrintMem()
	PrintRate()
}

func BenchmarkHeavyMixedInt_gache(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	var wg sync.WaitGroup
	for range 10000 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 8192 {
				gc.Set(Int64Key(int64(i)), i+1)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 8192 {
				gc.Get(Int64Key(int64(i)))
			}
		}()
	}
	wg.Wait()

	AddMem()
}

func BenchmarkPutInt_gache(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	for i := 0; i < b.N; i++ {
		gc.Set(Int64Key(int64(i)), i+1)
	}
}

func BenchmarkGetInt_gache(b *testing.B) {
	gc := New[string]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	gc.Set("0", "0")
	for i := 0; i < b.N; i++ {
		gc.Get("0")
	}
}

func BenchmarkPut1K_gache(b *testing.B) {
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	for i := 0; i < b.N; i++ {
		gc.Set(Int64Key(int64(i)), Data1K)
	}
}

func BenchmarkPut1M_gache(b *testing.B) {
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	for i := 0; i < b.N; i++ {
		gc.Set(Int64Key(int64(i)), Data1M)
	}
}

func BenchmarkPutTinyObject_gache(b *testing.B) {
	gc := New[dummyData]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	for i := 0; i < b.N; i++ {
		gc.Set(Int64Key(int64(i)), dummyData{})
	}
}

func BenchmarkChangeOutAllInt_gache(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	// slen = 512
	for i := 0; i < b.N*1024; i++ {
		gc.Set(Int64Key(int64(i)), i+1)
	}
}

func BenchmarkHeavyReadInt_gache(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	GCPause()

	// slen = 512
	for i := range 1024 {
		gc.Set(Int64Key(int64(i)), i+1)
	}
	var wg sync.WaitGroup
	for range 10000 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1024 {
				gc.Get(Int64Key(int64(i)))
			}
		}()
	}
	wg.Wait()

	AddGCPause()
}

func BenchmarkHeavyWriteInt_gache(b *testing.B) {
	gc := New[int]().SetDefaultExpire(10 * time.Second)
	GCPause()

	// slen = 512
	var wg sync.WaitGroup
	for index := range 10000 {
		start := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 8192 {
				gc.Set(Int64Key(int64(i+start)), i+1)
			}
		}()
	}
	wg.Wait()

	AddGCPause()
}

func BenchmarkHeavyWrite1K_gache(b *testing.B) {
	gc := New[[]byte]().SetDefaultExpire(10 * time.Second)
	GCPause()

	// slen = 512
	var wg sync.WaitGroup
	for index := range 10000 {
		start := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 8192 {
				gc.Set(Int64Key(int64(i+start)), Data1K)
			}
		}()
	}
	wg.Wait()

	AddGCPause()
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

var previousPause time.Duration

func GCPause() time.Duration {
	runtime.GC()
	var stats debug.GCStats
	debug.ReadGCStats(&stats)
	pause := stats.PauseTotal - previousPause
	previousPause = stats.PauseTotal
	return pause
}

var gcResult = make(map[string]time.Duration, 0)

func AddGCPause() {
	pc, _, _, _ := runtime.Caller(1)
	name := strings.Replace(runtime.FuncForPC(pc).Name(), "_", "GC_", 1)
	name = name[strings.Index(name, "Benchmark"):]
	if _, ok := gcResult[name]; !ok {
		gcResult[name] = GCPause()
	}
}

func PrintGCPause() {
	for k, v := range gcResult {
		fmt.Printf("%s-1 1 %d ns/op\n", k, v)
	}
}

func PrintMem() {
	for k, v := range memResult {
		fmt.Printf("%s-1 1 %d B\n", k, v)
	}
}

var memResult = make(map[string]uint64, 0)

func AddMem() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	pc, _, _, _ := runtime.Caller(1)
	name := strings.Replace(runtime.FuncForPC(pc).Name(), "_", "Mem_", 1)
	name = name[strings.Index(name, "Benchmark"):]
	if _, ok := memResult[name]; !ok {
		memResult[name] = ms.Sys
	}
}

var rateResult = make(map[string]float64, 0)

func AddRate(r float64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	pc, _, _, _ := runtime.Caller(1)
	name := runtime.FuncForPC(pc).Name()
	name = name[strings.Index(name, "Benchmark"):]
	if _, ok := rateResult[name]; !ok {
		rateResult[name] = r
	}
}

func PrintRate() {
	for k, v := range rateResult {
		fmt.Printf("%s-1 1 %.2f %%\n", k, 100.*v)
	}
}

type dummyData struct {
	Name    string
	Company string
	Skills  []string
	Age     int32
	Gender  int32
}

// BenchmarkDeleteExpired measures the throughput of the periodic cleanup sweep.
// It pre-fills the cache with expired entries so that every shard has work to do.
func BenchmarkDeleteExpired(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := New[string](
		WithDefaultExpiration[string](1 * time.Nanosecond),
	)
	// Pre-fill with keys that will expire immediately.
	for i := range numKeys {
		g.SetWithExpire(strconv.Itoa(i), "v", 1*time.Nanosecond)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		g.DeleteExpired(ctx)
		// Refill so next iteration has something to delete.
		for i := range numKeys {
			g.SetWithExpire(strconv.Itoa(i), "v", 1*time.Nanosecond)
		}
	}
}

// BenchmarkRange measures the throughput of iterating all cache entries.
func BenchmarkRange(b *testing.B) {
	const numKeys = 10000
	ctx := context.Background()
	g := New[string](
		WithDefaultExpiration[string](NoTTL),
	)
	for i := range numKeys {
		g.Set(strconv.Itoa(i), "v")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		g.Range(ctx, func(_ string, _ string, _ int64) bool { return true })
	}
}

func BenchmarkKeys(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Keys(ctx)
	}
}

func BenchmarkValues(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Values(ctx)
	}
}

func BenchmarkToRawMap(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := range 100000 {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.ToRawMap(ctx)
	}
}

func BenchmarkLoop(b *testing.B) {
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
			for i := 0; i < b.N; i++ {
				g.loop(ctx, func(_ int, k string, v *value[string]) bool {
					return true
				})
			}
		})
	}
}
