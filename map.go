package gache

import (
	"sync"
	"unsafe"
)

// Map is a thread-safe map implementation using RWMutex.
// It is designed to be "pointer-less" (GC friendly) when instantiated with value types.
// It supports zero-value usage (lazy initialization).
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// newMap creates a new Map.
func newMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		m: make(map[K]V),
	}
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	m.mu.RLock()
	if m.m == nil {
		m.mu.RUnlock()
		return value, false
	}
	value, ok = m.m[key]
	m.mu.RUnlock()
	return value, ok
}

// Store sets the value for a key.
func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	m.m[key] = value
	m.mu.Unlock()
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	actual, loaded = m.m[key]
	if !loaded {
		m.m[key] = value
		actual = value
	}
	m.mu.Unlock()
	return actual, loaded
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	m.mu.Lock()
	if m.m == nil {
		m.mu.Unlock()
		return value, false
	}
	value, loaded = m.m[key]
	if loaded {
		delete(m.m, key)
	}
	m.mu.Unlock()
	return value, loaded
}

// Delete deletes the value for a key.
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	if m.m == nil {
		m.mu.Unlock()
		return
	}
	delete(m.m, key)
	m.mu.Unlock()
}

// Swap swaps the value for a key and returns the previous value if any.
func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	m.mu.Lock()
	if m.m == nil {
		m.m = make(map[K]V)
	}
	previous, loaded = m.m[key]
	m.m[key] = value
	m.mu.Unlock()
	return previous, loaded
}

// CompareAndSwap swaps the old and new values for key if the value stored in the map is equal to old.
func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		return false
	}
	val, ok := m.m[key]
	if !ok {
		return false
	}
	if interface{}(val) == interface{}(old) {
		m.m[key] = new
		return true
	}
	return false
}

// CompareAndDelete deletes the entry for key if its value is equal to old.
func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		return false
	}
	val, ok := m.m[key]
	if !ok {
		return false
	}
	if interface{}(val) == interface{}(old) {
		delete(m.m, key)
		return true
	}
	return false
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
// We snapshot keys to allow safe modification within the callback.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.mu.RLock()
	if m.m == nil {
		m.mu.RUnlock()
		return
	}
	keys := make([]K, 0, len(m.m))
	for k := range m.m {
		keys = append(keys, k)
	}
	m.mu.RUnlock()

	for _, k := range keys {
		// We use Load to get the value safely (and check if it was deleted)
		v, ok := m.Load(k)
		if !ok {
			continue
		}
		if !f(k, v) {
			break
		}
	}
}

// Clear clears the map.
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	m.m = make(map[K]V)
	m.mu.Unlock()
}

// Len returns the number of items in the map.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}

// Size returns the approximate size of the map in bytes.
func (m *Map[K, V]) Size() uintptr {
	m.mu.RLock()
	defer m.mu.RUnlock()
	size := unsafe.Sizeof(m.mu) + unsafe.Sizeof(m.m)
	if m.m != nil {
		size += mapSize(m.m)
	}
	return size
}
