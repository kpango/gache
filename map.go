package gache

import (
	"sync"
	"unsafe"
	"reflect"
)

type slot[K comparable, V any] struct {
	key    K
	val    V
	expire int64
	hash   uint64
}

type Map[K comparable, V any] struct {
	_        [64]byte // padding
	mu       sync.RWMutex
	items    []slot[K, V]
	occupied []bool
	count    int
	mask     uint64
	_        [64]byte // padding
}

func (m *Map[K, V]) init(size int) {
	if size < 8 { size = 8 }
	capacity := 1
	for capacity < size { capacity <<= 1 }
	m.items = make([]slot[K, V], capacity)
	m.occupied = make([]bool, capacity)
	m.mask = uint64(capacity - 1)
	m.count = 0
}

// hashKey avoids allocation for strings by exploiting string header
func hashKey[K comparable](key K) uint64 {
	// Fast path for string, without allocating interface{}
	// Since K is generic, we can check if it's a string by unsafe inspection of types
	// But type reflection on *new(K) is safer and only evaluated once per type via compiler optimization
	var k interface{} = key // boxes value
	switch v := k.(type) {
	case string:
		var h uint64 = 14695981039346656037
		for i := 0; i < len(v); i++ {
			h ^= uint64(v[i])
			h *= 1099511628211
		}
		return h
	case int:
		return uint64(v) * 2654435761
	}
	return 0
}

func (m *Map[K, V]) findIndex(key K, h uint64) (uint64, bool) {
	if len(m.items) == 0 { return 0, false }
	idx := h & m.mask
	for {
		if !m.occupied[idx] { return idx, false }
		if m.items[idx].hash == h && m.items[idx].key == key { return idx, true }
		idx = (idx + 1) & m.mask
	}
}

func (m *Map[K, V]) resize() {
	oldItems := m.items
	oldOccupied := m.occupied
	m.init(len(m.items) * 2)
	for i, occ := range oldOccupied {
		if occ {
			slot := oldItems[i]
			idx, _ := m.findIndex(slot.key, slot.hash)
			m.items[idx] = slot
			m.occupied[idx] = true
			m.count++
		}
	}
}

func (m *Map[K, V]) Load(key K) (V, bool) {
	m.mu.RLock()
	if len(m.items) > 0 {
		h := hashKey(key)
		if idx, found := m.findIndex(key, h); found {
			val := m.items[idx].val
			m.mu.RUnlock()
			return val, true
		}
	}
	m.mu.RUnlock()
	var v V
	return v, false
}

func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	if len(m.items) == 0 {
		m.init(8)
	} else if float64(m.count)/float64(len(m.items)) > 0.75 {
		m.resize()
	}
	h := hashKey(key)
	idx, found := m.findIndex(key, h)
	if !found {
		m.occupied[idx] = true
		m.items[idx].key = key
		m.items[idx].hash = h
		m.count++
	}
	m.items[idx].val = value
	m.mu.Unlock()
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (V, bool) {
	m.mu.Lock()
	if len(m.items) == 0 {
		m.init(8)
	} else if float64(m.count)/float64(len(m.items)) > 0.75 {
		m.resize()
	}
	h := hashKey(key)
	idx, found := m.findIndex(key, h)
	if found {
		val := m.items[idx].val
		m.mu.Unlock()
		return val, true
	}
	m.occupied[idx] = true
	m.items[idx].key = key
	m.items[idx].hash = h
	m.items[idx].val = value
	m.count++
	m.mu.Unlock()
	return value, false
}

func (m *Map[K, V]) shift(idx uint64) {
	m.occupied[idx] = false
	var empty V
	m.items[idx].val = empty
	m.items[idx].key = *new(K)
	i := (idx + 1) & m.mask
	for m.occupied[i] {
		h := m.items[i].hash
		desired := h & m.mask
		distToDesired := (i - desired) & m.mask
		distToEmpty := (i - idx) & m.mask
		if distToDesired >= distToEmpty {
			m.items[idx] = m.items[i]
			m.occupied[idx] = true
			m.occupied[i] = false
			m.items[i].val = empty
			m.items[i].key = *new(K)
			idx = i
		}
		i = (i + 1) & m.mask
	}
}

func (m *Map[K, V]) LoadAndDelete(key K) (V, bool) {
	m.mu.Lock()
	if len(m.items) > 0 {
		h := hashKey(key)
		if idx, found := m.findIndex(key, h); found {
			val := m.items[idx].val
			m.shift(idx)
			m.count--
			m.mu.Unlock()
			return val, true
		}
	}
	m.mu.Unlock()
	var v V
	return v, false
}

func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	if len(m.items) > 0 {
		h := hashKey(key)
		if idx, found := m.findIndex(key, h); found {
			m.shift(idx)
			m.count--
		}
	}
	m.mu.Unlock()
}

func (m *Map[K, V]) Swap(key K, value V) (V, bool) {
	m.mu.Lock()
	if len(m.items) == 0 { m.init(8) } else if float64(m.count)/float64(len(m.items)) > 0.75 { m.resize() }
	h := hashKey(key)
	idx, found := m.findIndex(key, h)
	var prev V
	if found {
		prev = m.items[idx].val
	} else {
		m.occupied[idx] = true
		m.items[idx].key = key
		m.items[idx].hash = h
		m.count++
	}
	m.items[idx].val = value
	m.mu.Unlock()
	return prev, found
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) bool {
	m.mu.Lock()
	if len(m.items) > 0 {
		h := hashKey(key)
		if idx, found := m.findIndex(key, h); found && reflect.DeepEqual(m.items[idx].val, old) {
			m.items[idx].val = new
			m.mu.Unlock()
			return true
		}
	}
	m.mu.Unlock()
	return false
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) bool {
	m.mu.Lock()
	if len(m.items) > 0 {
		h := hashKey(key)
		if idx, found := m.findIndex(key, h); found && reflect.DeepEqual(m.items[idx].val, old) {
			m.shift(idx)
			m.count--
			m.mu.Unlock()
			return true
		}
	}
	m.mu.Unlock()
	return false
}

func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	m.items = nil
	m.occupied = nil
	m.count = 0
	m.mask = 0
	m.mu.Unlock()
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.mu.RLock()
	if len(m.items) == 0 {
		m.mu.RUnlock()
		return
	}
	keys := make([]K, 0, m.count)
	for i, occ := range m.occupied {
		if occ { keys = append(keys, m.items[i].key) }
	}
	m.mu.RUnlock()
	for _, k := range keys {
		m.mu.RLock()
		h := hashKey(k)
		idx, found := m.findIndex(k, h)
		var val V
		if found { val = m.items[idx].val }
		m.mu.RUnlock()
		if found {
			if !f(k, val) { break }
		}
	}
}

func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	c := m.count
	m.mu.RUnlock()
	return c
}

func (m *Map[K, V]) Size() uintptr {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var empty V
	return unsafe.Sizeof(*m) + uintptr(len(m.items))*unsafe.Sizeof(slot[K, V]{key: *new(K), val: empty}) + uintptr(len(m.occupied))*unsafe.Sizeof(bool(false))
}
