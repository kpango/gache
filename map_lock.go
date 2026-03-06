package gache

import (
	"reflect"
	"sync"
	"unsafe"
)

type MapLock[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func (m *MapLock[K, V]) Load(key K) (value V, ok bool) {
	m.mu.RLock()
	value, ok = m.m[key]
	m.mu.RUnlock()
	return value, ok
}

func (m *MapLock[K, V]) Store(key K, value V) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	m.m[key] = value
	m.mu.Unlock()
}

func (m *MapLock[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	m.mu.RLock()
	actual, loaded = m.m[key]
	m.mu.RUnlock()
	if loaded {
		return actual, loaded
	}

	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	actual, loaded = m.m[key]
	if !loaded {
		actual = value
		m.m[key] = value
	}
	m.mu.Unlock()
	return actual, loaded
}

func (m *MapLock[K, V]) LoadOrStorePtr(key K, value V) (actual *V, loaded bool) {
	// Not used in gache for MapLock normally, but needed for interface compliance if needed.
	m.mu.RLock()
	v, loaded := m.m[key]
	m.mu.RUnlock()
	if loaded {
		return &v, loaded
	}

	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	v, loaded = m.m[key]
	if !loaded {
		m.m[key] = value
		actual = &value
	} else {
		actual = &v
	}
	m.mu.Unlock()
	return actual, loaded
}

func (m *MapLock[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	previous, loaded = m.m[key]
	m.m[key] = value
	m.mu.Unlock()
	return previous, loaded
}

func (m *MapLock[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	m.mu.RLock()
	value, loaded = m.m[key]
	m.mu.RUnlock()
	if !loaded {
		return value, loaded
	}

	m.mu.Lock()
	value, loaded = m.m[key]
	if loaded {
		delete(m.m, key)
	}
	m.mu.Unlock()
	return value, loaded
}

func (m *MapLock[K, V]) Delete(key K) {
	m.mu.Lock()
	if m.m != nil {
		delete(m.m, key)
	}
	m.mu.Unlock()
}

func (m *MapLock[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		return false
	}
	val, ok := m.m[key]
	if !ok {
		return false
	}

	if reflect.DeepEqual(val, old) {
		m.m[key] = new
		return true
	}
	return false
}

func (m *MapLock[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		return false
	}
	val, ok := m.m[key]
	if !ok {
		return false
	}
	if reflect.DeepEqual(val, old) {
		delete(m.m, key)
		return true
	}
	return false
}

func (m *MapLock[K, V]) Range(f func(key K, value V) bool) {
	m.mu.RLock()
	if m.m == nil {
		m.mu.RUnlock()
		return
	}
	for k, v := range m.m {
		if !f(k, v) {
			break
		}
	}
	m.mu.RUnlock()
}

func (m *MapLock[K, V]) Clear() {
	m.mu.Lock()
	m.m = nil
	m.mu.Unlock()
}

func (m *MapLock[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}

func (m *MapLock[K, V]) Size() (size uintptr) {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	size = unsafe.Sizeof(m.mu)

	size += mapSize(m.m)
	for _, v := range m.m {
		if sizer, ok := any(v).(interface{ Size() uintptr }); ok {
			size += sizer.Size()
		} else {
			// Estimate size for *value[V]
			size += unsafe.Sizeof(v)
		}
	}
	return size
}
