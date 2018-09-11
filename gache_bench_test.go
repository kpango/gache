package gache

import (
	"sync"
	"testing"
	"time"

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
	data = map[string]string{
		"string": "aaaa",
		"int":    "123",
		"float":  "99.99",
		"struct": "struct{}{}",
	}
	// data = map[string]interface{}{
	// 	"string": "aaaa",
	// 	"int":    123,
	// 	"float":  99.99,
	// 	"struct": struct{}{},
	// }
)

func BenchmarkGache(b *testing.B) {
	New()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
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

func BenchmarkMap(b *testing.B) {
	m := NewDefault()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				m.Set(k, v)

				val, ok := m.Get(k)
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

func BenchmarkBigCache(b *testing.B) {
	cfg := bigcache.DefaultConfig(10 * time.Minute)
	cfg.Verbose = false
	c, _ := bigcache.NewBigCache(cfg)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
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

func BenchmarkGoCache(b *testing.B) {
	c := cache.New(5*time.Minute, 10*time.Minute)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
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

func BenchmarkGCacheLRU(b *testing.B) {
	gc := gcache.New(20).
		LRU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}
func BenchmarkGCacheLFU(b *testing.B) {
	gc := gcache.New(20).
		LFU().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkGCacheARC(b *testing.B) {
	gc := gcache.New(20).
		ARC().
		Build()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				gc.SetWithExpire(k, v, time.Second*30)
				val, err := gc.Get(k)
				if err != nil {
					b.Errorf("GCache Get failed key: %v\tval: %v\n", k, v)
				}
				if val != v {
					b.Errorf("expect %v but got %v", v, val)
				}
			}
		}
	})
}

func BenchmarkFreeCache(b *testing.B) {
	fc := freecache.NewCache(100 * 1024 * 1024)
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
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

func BenchmarkGocache(b *testing.B) {
	gc := gocache.New()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
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
