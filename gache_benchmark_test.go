package gache

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

type DefaultMap struct {
	mu   sync.RWMutex
	data map[interface{}]interface{}
}

func NewDefault() *DefaultMap {
	return &DefaultMap{
		data: make(map[interface{}]interface{}),
	}
}

func (m *DefaultMap) Get(key interface{}) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *DefaultMap) Set(key, val interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
}

var (
	ttl time.Duration = 50 * time.Millisecond

	parallelism = 10000

	bigData      = map[string]string{}
	bigDataLen   = 2 << 10
	bigDataCount = 2 << 16

	smallData = map[string]string{
		"string": "aaaa",
		"int":    "123",
		"float":  "99.99",
		"struct": "struct{}{}",
	}
)

func init() {
	for i := 0; i < bigDataCount; i++ {
		bigData[randStr(bigDataLen)] = randStr(bigDataLen)
	}
}

var randSrc = rand.NewSource(time.Now().UnixNano())

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
	return *(*string)(unsafe.Pointer(&b))
}

func benchmark(b *testing.B, data map[string]string,
	t time.Duration,
	set func(string, string, time.Duration),
	get func(string),
) {
	b.Helper()
	b.SetParallelism(parallelism)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				set(k, v, t)
			}
			for k := range data {
				get(k)
			}
		}
	})
}

func BenchmarkDefaultMapSetSmallDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

func BenchmarkDefaultMapSetBigDataNoTTL(b *testing.B) {
	m := NewDefault()
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Set(k, v) },
		func(k string) { m.Get(k) })
}

func BenchmarkSyncMapSetSmallDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

func BenchmarkSyncMapSetBigDataNoTTL(b *testing.B) {
	var m sync.Map
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { m.Store(k, v) },
		func(k string) { m.Load(k) })
}

func BenchmarkGacheSetSmallDataNoTTL(b *testing.B) {
	g := New[string](
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, smallData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheSetSmallDataWithTTL(b *testing.B) {
	g := New(
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, smallData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheSetBigDataNoTTL(b *testing.B) {
	g := New(
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}

func BenchmarkGacheSetBigDataWithTTL(b *testing.B) {
	g := New(
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, bigData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}

func TestMain(m *testing.M) {
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
	for index := 0; index < 10000; index++ {
		wg.Add(1)
		go func() {
			for i := 0; i < 8192; i++ {
				gc.Set(Int64Key(int64(i)), i+1)
			}
			wg.Done()
		}()
		wg.Add(1)
		go func() {
			for i := 0; i < 8192; i++ {
				gc.Get(Int64Key(int64(i)))
			}
			wg.Done()
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
	for i := 0; i < 1024; i++ {
		gc.Set(Int64Key(int64(i)), i+1)
	}
	var wg sync.WaitGroup
	for index := 0; index < 10000; index++ {
		wg.Add(1)
		go func() {
			for i := 0; i < 1024; i++ {
				gc.Get(Int64Key(int64(i)))
			}
			wg.Done()
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
	for index := 0; index < 10000; index++ {
		start := index
		wg.Add(1)
		go func() {
			for i := 0; i < 8192; i++ {
				gc.Set(Int64Key(int64(i+start)), i+1)
			}
			wg.Done()
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
	for index := 0; index < 10000; index++ {
		start := index
		wg.Add(1)
		go func() {
			for i := 0; i < 8192; i++ {
				gc.Set(Int64Key(int64(i+start)), Data1K)
			}
			wg.Done()
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
	Age     int32
	Gender  int32
	Company string
	Skills  []string
}
