package gache

import (
	"sync"
	"testing"
)

var (
	data = map[string]interface{}{
		"string": "aaaa",
		"int":    123,
		"float":  99.99,
		"struct": struct{}{},
	}
)

func BenchmarkGache(b *testing.B) {
	New()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				ok := Set(k, v)
				if !ok {
					b.Errorf("Gache Set failed key: %v\tval: %v\n", k, v)
				}
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
	m := make(map[interface{}]interface{})
	var mu sync.RWMutex
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for k, v := range data {
				mu.Lock()
				m[k] = v
				mu.Unlock()

				mu.RLock()
				val, ok := m[k]
				mu.RUnlock()
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
