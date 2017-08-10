package gache

import (
	"sync"
	"testing"
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

	if !ok {
		return nil, false
	}

	return v, true
}

func (m *DefaultMap) Set(key, val interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = val
}

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
