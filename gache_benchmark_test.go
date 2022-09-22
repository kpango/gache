package gache

import (
	"math/rand"
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
	get func(string)) {
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
	g := New[string](
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, smallData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}
func BenchmarkGacheSetBigDataNoTTL(b *testing.B) {
	g := New[string](
		WithDefaultExpiration[string](NoTTL),
	)
	benchmark(b, bigData, NoTTL,
		func(k, v string, t time.Duration) { g.Set(k, v) },
		func(k string) { g.Get(k) })
}
func BenchmarkGacheSetBigDataWithTTL(b *testing.B) {
	g := New[string](
		WithDefaultExpiration[string](ttl),
	)
	benchmark(b, bigData, ttl,
		func(k, v string, t time.Duration) { g.SetWithExpire(k, v, t) },
		func(k string) { g.Get(k) })
}
