package gache

import (
	"hash/maphash"
	"reflect"
	"sync"
	"unsafe"
)

// Map is a generic, open-addressing hash table with linear probing.
// Protected by sync.RWMutex.
// Utilizes explicit 64-byte struct padding to prevent CPU cache-line false sharing between shards.
type Map[K comparable, V any] struct {
	mu    sync.RWMutex
	slots []slot[K, V]
	used  []bool
	len   int
	seed  maphash.Seed
	_     [64]byte // padding to prevent false sharing
}

type slot[K comparable, V any] struct {
	key K
	val V
}

func newMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		seed: maphash.MakeSeed(),
	}
}

func (m *Map[K, V]) InitReserve(size int) {
	m.mu.Lock()
	if len(m.slots) == 0 && size > 0 {
		m.slots = make([]slot[K, V], size)
		m.used = make([]bool, size)
	}
	m.mu.Unlock()
}

func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	c := m.len
	m.mu.RUnlock()
	return c
}

func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	m.slots = nil
	m.used = nil
	m.len = 0
	m.mu.Unlock()
}

func (m *Map[K, V]) Size() uintptr {
	m.mu.RLock()
	size := unsafe.Sizeof(*m)
	if cap(m.slots) > 0 {
		size += uintptr(cap(m.slots)) * unsafe.Sizeof(m.slots[0])
		size += uintptr(cap(m.used)) * unsafe.Sizeof(m.used[0])
	}
	m.mu.RUnlock()
	return size
}

func (m *Map[K, V]) find(key K) (int, bool) {
	if len(m.slots) == 0 {
		return 0, false
	}
	hash := maphash.Comparable(m.seed, key)
	mask := uint64(len(m.slots) - 1)
	idx := hash & mask

	for {
		if !m.used[idx] {
			return int(idx), false
		}
		if m.slots[idx].key == key {
			return int(idx), true
		}
		idx = (idx + 1) & mask
	}
}

func (m *Map[K, V]) resize() {
	oldSlots := m.slots
	oldUsed := m.used

	newCap := len(m.slots) * 2
	if newCap == 0 {
		newCap = 8 // Initial capacity
	}

	m.slots = make([]slot[K, V], newCap)
	m.used = make([]bool, newCap)
	m.len = 0
	mask := uint64(newCap - 1)

	for i, used := range oldUsed {
		if used {
			key := oldSlots[i].key
			val := oldSlots[i].val
			hash := maphash.Comparable(m.seed, key)
			idx := hash & mask
			for m.used[idx] {
				idx = (idx + 1) & mask
			}
			m.slots[idx] = slot[K, V]{key: key, val: val}
			m.used[idx] = true
			m.len++
		}
	}
}

func (m *Map[K, V]) Load(key K) (V, bool) {
	m.mu.RLock()
	if len(m.slots) == 0 {
		m.mu.RUnlock()
		var zero V
		return zero, false
	}
	idx, ok := m.find(key)
	if !ok {
		m.mu.RUnlock()
		var zero V
		return zero, false
	}
	val := m.slots[idx].val
	m.mu.RUnlock()
	return val, true
}

func (m *Map[K, V]) LoadPointer(key K) (*V, bool) {
	m.mu.RLock()
	if len(m.slots) == 0 {
		m.mu.RUnlock()
		return nil, false
	}
	idx, ok := m.find(key)
	if !ok {
		m.mu.RUnlock()
		return nil, false
	}
	val := &m.slots[idx].val
	m.mu.RUnlock()
	return val, true
}

func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	if len(m.slots) == 0 || float64(m.len)/float64(len(m.slots)) > 0.75 {
		m.resize()
	}

	idx, ok := m.find(key)
	if ok {
		m.slots[idx].val = value
		m.mu.Unlock()
		return
	}

	hash := maphash.Comparable(m.seed, key)
	mask := uint64(len(m.slots) - 1)
	idxU := hash & mask
	for m.used[idxU] {
		idxU = (idxU + 1) & mask
	}

	m.slots[idxU] = slot[K, V]{key: key, val: value}
	m.used[idxU] = true
	m.len++
	m.mu.Unlock()
}

func (m *Map[K, V]) StorePointer(key K, value *V) {
	m.Store(key, *value)
}

func (m *Map[K, V]) Swap(key K, value V) (V, bool) {
	m.mu.Lock()
	if len(m.slots) == 0 || float64(m.len)/float64(len(m.slots)) > 0.75 {
		m.resize()
	}

	idx, ok := m.find(key)
	if ok {
		old := m.slots[idx].val
		m.slots[idx].val = value
		m.mu.Unlock()
		return old, true
	}

	hash := maphash.Comparable(m.seed, key)
	mask := uint64(len(m.slots) - 1)
	idxU := hash & mask
	for m.used[idxU] {
		idxU = (idxU + 1) & mask
	}

	m.slots[idxU] = slot[K, V]{key: key, val: value}
	m.used[idxU] = true
	m.len++
	m.mu.Unlock()
	var zero V
	return zero, false
}

func (m *Map[K, V]) SwapPointer(key K, value *V) (*V, bool) {
	v, ok := m.Swap(key, *value)
	if ok {
		return &v, true
	}
	return nil, false
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (V, bool) {
	m.mu.RLock()
	idx, ok := m.find(key)
	if ok {
		val := m.slots[idx].val
		m.mu.RUnlock()
		return val, true
	}
	m.mu.RUnlock()

	m.mu.Lock()
	if len(m.slots) == 0 || float64(m.len)/float64(len(m.slots)) > 0.75 {
		m.resize()
	}

	idx, ok = m.find(key)
	if ok {
		val := m.slots[idx].val
		m.mu.Unlock()
		return val, true
	}

	hash := maphash.Comparable(m.seed, key)
	mask := uint64(len(m.slots) - 1)
	idxU := hash & mask
	for m.used[idxU] {
		idxU = (idxU + 1) & mask
	}

	m.slots[idxU] = slot[K, V]{key: key, val: value}
	m.used[idxU] = true
	m.len++
	m.mu.Unlock()
	return value, false
}

func (m *Map[K, V]) LoadOrStorePointer(key K, value *V) (*V, bool) {
	v, loaded := m.LoadOrStore(key, *value)
	return &v, loaded
}

func (m *Map[K, V]) LoadAndDelete(key K) (V, bool) {
	m.mu.Lock()
	idx, ok := m.find(key)
	if !ok {
		m.mu.Unlock()
		var zero V
		return zero, false
	}

	val := m.slots[idx].val
	m.used[idx] = false
	var zeroKey K
	var zeroVal V
	m.slots[idx] = slot[K, V]{key: zeroKey, val: zeroVal}
	m.len--

	mask := uint64(len(m.slots) - 1)
	i := (uint64(idx) + 1) & mask
	for m.used[i] {
		k := m.slots[i].key
		v := m.slots[i].val
		m.used[i] = false
		m.slots[i] = slot[K, V]{key: zeroKey, val: zeroVal}
		m.len--

		h := maphash.Comparable(m.seed, k)
		j := h & mask
		for m.used[j] {
			j = (j + 1) & mask
		}
		m.slots[j] = slot[K, V]{key: k, val: v}
		m.used[j] = true
		m.len++

		i = (i + 1) & mask
	}

	m.mu.Unlock()
	return val, true
}

func (m *Map[K, V]) LoadAndDeletePointer(key K) (*V, bool) {
	v, ok := m.LoadAndDelete(key)
	if ok {
		return &v, true
	}
	return nil, false
}

func (m *Map[K, V]) Delete(key K) {
	m.LoadAndDelete(key)
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.mu.RLock()
	var snapshot []slot[K, V]
	if m.len > 0 {
		snapshot = make([]slot[K, V], 0, m.len)
		for i, used := range m.used {
			if used {
				snapshot = append(snapshot, m.slots[i])
			}
		}
	}
	m.mu.RUnlock()

	for _, s := range snapshot {
		if !f(s.key, s.val) {
			break
		}
	}
}

func (m *Map[K, V]) RangePointer(f func(key K, value *V) bool) {
	m.mu.RLock()
	var snapshot []slot[K, V]
	if m.len > 0 {
		snapshot = make([]slot[K, V], 0, m.len)
		for i, used := range m.used {
			if used {
				snapshot = append(snapshot, m.slots[i])
			}
		}
	}
	m.mu.RUnlock()

	for i := range snapshot {
		s := &snapshot[i]
		if !f(s.key, &s.val) {
			break
		}
	}
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) bool {
	m.mu.Lock()
	idx, ok := m.find(key)
	if ok && reflect.DeepEqual(m.slots[idx].val, old) {
		m.slots[idx].val = new
		m.mu.Unlock()
		return true
	}
	m.mu.Unlock()
	return false
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) bool {
	m.mu.Lock()
	idx, ok := m.find(key)
	if ok && reflect.DeepEqual(m.slots[idx].val, old) {
		m.used[idx] = false
		var zeroKey K
		var zeroVal V
		m.slots[idx] = slot[K, V]{key: zeroKey, val: zeroVal}
		m.len--

		mask := uint64(len(m.slots) - 1)
		i := (uint64(idx) + 1) & mask
		for m.used[i] {
			k := m.slots[i].key
			v := m.slots[i].val
			m.used[i] = false
			m.slots[i] = slot[K, V]{key: zeroKey, val: zeroVal}
			m.len--

			h := maphash.Comparable(m.seed, k)
			j := h & mask
			for m.used[j] {
				j = (j + 1) & mask
			}
			m.slots[j] = slot[K, V]{key: k, val: v}
			m.used[j] = true
			m.len++

			i = (i + 1) & mask
		}
		m.mu.Unlock()
		return true
	}
	m.mu.Unlock()
	return false
}

func (m *Map[K, V]) CompareAndSwapPointer(key K, old, new *V) bool {
	return m.CompareAndSwap(key, *old, *new)
}
