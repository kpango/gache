package gache

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	mcache "github.com/OrlovEvgeny/go-mcache"
	"github.com/allegro/bigcache"
	"github.com/bluele/gcache"
	"github.com/coocood/freecache"
	"github.com/hlts2/gocache"
	cache "github.com/patrickmn/go-cache"
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
	m.data[key] = val
}

var (
	bigData    = map[string]string{}
	bigDataLen = 10000

	smallData = map[string]string{
		"string": "aaaa",
		"int":    "123",
		"float":  "99.99",
		"struct": "struct{}{}",
	}
)

func init() {
	for i := 0; i < bigDataLen; i++ {
		bigData[randStr(i)] = randStr(1000)
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
	return string(b)
}

func BenchmarkGacheWithSmallDataset(b *testing.B) {
	GetGache()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				Set(k, v)
				val, ok := Get(k)
				if !ok {
					b.Errorf("Gache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGacheWithBigDataset(b *testing.B) {
	GetGache()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				Set(k, v)
				val, ok := Get(k)
				if !ok {
					b.Errorf("Gache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGocacheWithSmallDataset(b *testing.B) {
	gc := gocache.New()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				gc.Set(k, v)
				val, ok := gc.Get(k)
				if !ok {
					b.Errorf("GoCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGocacheWithBigDataset(b *testing.B) {
	gc := gocache.New()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				gc.Set(k, v)
				val, ok := gc.Get(k)
				if !ok {
					b.Errorf("GoCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkMapWithSmallDataset(b *testing.B) {
	m := NewDefault()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				m.Set(k, v)

				val, ok := m.Get(k)
				if !ok {
					b.Errorf("Map Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkMapWithBigDataset(b *testing.B) {
	m := NewDefault()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				m.Set(k, v)

				val, ok := m.Get(k)
				if !ok {
					b.Errorf("Map Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGoCacheWithSmallDataset(b *testing.B) {
	c := cache.New(5*time.Minute, 10*time.Minute)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				c.Set(k, v, cache.DefaultExpiration)
				val, ok := c.Get(k)
				if !ok {
					b.Errorf("Go-Cache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGoCacheWithBigDataset(b *testing.B) {
	c := cache.New(5*time.Minute, 10*time.Minute)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				c.Set(k, v, cache.DefaultExpiration)
				val, ok := c.Get(k)
				if !ok {
					b.Errorf("Go-Cache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheLRUWithSmallDataset(b *testing.B) {
	gc := gcache.New(20).
		LRU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheLRUWithBigDataset(b *testing.B) {
	gc := gcache.New(20).
		LRU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheLFUWithSmallDataset(b *testing.B) {
	gc := gcache.New(20).
		LFU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheLFUWithBigDataset(b *testing.B) {
	gc := gcache.New(20).
		LFU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheARCWithSmallDataset(b *testing.B) {
	gc := gcache.New(20).
		ARC().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheARCWithBigDataset(b *testing.B) {
	gc := gcache.New(20).
		ARC().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					// XXX gcache has a problem . it cannot get long keyname
					// b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					// b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkFreeCacheWithSmallDataset(b *testing.B) {
	fc := freecache.NewCache(100 * 1024 * 1024)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				fc.Set([]byte(k), []byte(v), 0)
				val, err := fc.Get([]byte(k))
				if err != nil {
					b.Errorf("FreeCache Get failed key: %v\tval: %v\n", k, v)
					b.Error(err)
				}

				if string(val) != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkFreeCacheWithBigDataset(b *testing.B) {
	fc := freecache.NewCache(100 * 1024 * 1024)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				fc.Set([]byte(k), []byte(v), 0)
				val, err := fc.Get([]byte(k))
				if err != nil {
					b.Errorf("FreeCache Get failed key: %v\tval: %v\n", k, v)
					b.Error(err)
				}

				if string(val) != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkBigCacheWithSmallDataset(b *testing.B) {
	cfg := bigcache.DefaultConfig(10 * time.Minute)
	cfg.Verbose = false
	c, _ := bigcache.NewBigCache(cfg)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				c.Set(k, []byte(v))
				val, err := c.Get(k)
				if err != nil {
					b.Errorf("BigCahce Get failed key: %v\tval: %v\n", k, v)
				}
				if string(val) != v {
					b.Errorf("expect %v but got %v", v, string(val))
				}
			}
		}
	})
}

func BenchmarkBigCacheWithBigDataset(b *testing.B) {
	cfg := bigcache.DefaultConfig(10 * time.Minute)
	cfg.Verbose = false
	c, _ := bigcache.NewBigCache(cfg)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				c.Set(k, []byte(v))
				val, err := c.Get(k)
				if err != nil {
					b.Errorf("BigCahce Get failed key: %v\tval: %v\n", k, v)
				}
				if string(val) != v {
					b.Errorf("expect %v but got %v", v, string(val))
				}
			}
		}
	})
}

func BenchmarkMCacheWithSmallDataset(b *testing.B) {
	mc := mcache.StartInstance()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range smallData {
				mc.SetPointer(k, v, time.Second*30)
				val, ok := mc.GetPointer(k)
				if !ok {
					b.Errorf("mcache Get failed key: %v\tval: %v\n", k, v)
				}
				if val.(string) != v {
					b.Errorf("expect %v but got %v", v, val.(string))
				}
			}
		}
	})
}

func BenchmarkMCacheWithBigDataset(b *testing.B) {
	mc := mcache.StartInstance()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range bigData {
				mc.SetPointer(k, v, time.Second*30)
				val, ok := mc.GetPointer(k)
				if !ok {
					b.Errorf("mcache Get failed key: %v\tval: %v\n", k, v)
				}
				if val.(string) != v {
					b.Errorf("expect %v but got %v", v, val.(string))
				}
			}
		}
	})
}
