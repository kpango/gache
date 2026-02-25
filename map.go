package gache

import (
	"sync"
	"unsafe"
)

// wheelSize determines the number of buckets in the timing wheel.
// 4096 slots covers ~1 hour with 1s resolution.
// Mask must be wheelSize - 1 (power of 2).
const wheelSize = 4096
const wheelMask = wheelSize - 1

type Map[V any] struct {
	mu sync.RWMutex

	// Arena storage for items
	items []store[V]

	// Lookup map: Key -> Index in items
	lookup map[string]int

	// Free list of indices for reuse
	free []int

	// Timing Wheel: buckets[i] is the index of the first item (head) in the linked list
	buckets [wheelSize]int
}

type store[V any] struct {
	key    string
	val    V
	expire int64

	// Doubly linked list for Timing Wheel
	prev, next int // -1 if nil
	bucketIdx  int // -1 if not in wheel
}

// NewMap creates a new Map instance
func NewMap[V any]() *Map[V] {
	m := &Map[V]{
		lookup: make(map[string]int),
		// Pre-allocate some items to avoid initial reallocations
		items: make([]store[V], 0, 1024),
		free:  make([]int, 0, 128),
	}
	// Initialize buckets to -1 (empty)
	for i := range m.buckets {
		m.buckets[i] = -1
	}
	return m
}

func (m *Map[V]) Load(key string, now int64) (val V, expire int64, ok bool) {
	m.mu.RLock()
	idx, exists := m.lookup[key]
	if !exists {
		m.mu.RUnlock()
		return val, 0, false
	}
	item := &m.items[idx]
	v := item.val
	exp := item.expire
	m.mu.RUnlock()
	return v, exp, true
}

func (m *Map[V]) Store(key string, val V, expire int64) (isNew bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, exists := m.lookup[key]
	if exists {
		// Update existing item
		item := &m.items[idx]
		item.val = val

		// If expiration changed, move in wheel
		if item.expire != expire {
			m.removeFromWheel(idx)
			item.expire = expire
			m.addToWheel(idx, expire)
		}
		return false
	}

	// New item
	if len(m.free) > 0 {
		// Reuse free slot
		last := len(m.free) - 1
		idx = m.free[last]
		m.free = m.free[:last]

		// Initialize slot
		m.items[idx] = store[V]{
			key:       key,
			val:       val,
			expire:    expire,
			prev:      -1,
			next:      -1,
			bucketIdx: -1,
		}
	} else {
		// Append new slot
		idx = len(m.items)
		m.items = append(m.items, store[V]{
			key:       key,
			val:       val,
			expire:    expire,
			prev:      -1,
			next:      -1,
			bucketIdx: -1,
		})
	}

	m.lookup[key] = idx
	m.addToWheel(idx, expire)
	return true
}

func (m *Map[V]) Delete(key string) (val V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, exists := m.lookup[key]
	if !exists {
		return val, false
	}

	item := &m.items[idx]
	val = item.val

	m.removeFromWheel(idx)
	delete(m.lookup, key)

	// Zero out the item to release references
	var zeroV V
	item.val = zeroV
	item.key = ""
	item.expire = 0

	m.free = append(m.free, idx)

	return val, true
}

func (m *Map[V]) Range(f func(string, V, int64) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for k, idx := range m.lookup {
		item := &m.items[idx]
		if !f(k, item.val, item.expire) {
			break
		}
	}
}

func (m *Map[V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset slices
	m.items = m.items[:0]
	m.free = m.free[:0]
	// Re-make map
	m.lookup = make(map[string]int)

	for i := range m.buckets {
		m.buckets[i] = -1
	}
}

func (m *Map[V]) Size() uintptr {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Approx size
	size := unsafe.Sizeof(*m)
	size += uintptr(cap(m.items)) * unsafe.Sizeof(store[V]{})
	size += uintptr(cap(m.free)) * unsafe.Sizeof(int(0))

	// Minimal map estimation without using unsafe runtime structures
	// 10.79 * len seems to be a rough heuristic for map overhead in some contexts, but let's stick to simple key/value size
	// Map overhead is usually bucket count * bucket size.
	// We just estimate key + int + overhead per entry.
	// map[string]int
	size += uintptr(len(m.lookup)) * (unsafe.Sizeof("") + unsafe.Sizeof(int(0)) + 16)

	return size
}

// Wheel Helpers (Not thread-safe, caller must hold lock)

func (m *Map[V]) addToWheel(idx int, expire int64) {
	if expire <= 0 {
		m.items[idx].bucketIdx = -1
		return
	}

	// Calculate bucket
	seconds := expire / 1e9
	bucket := int(seconds & wheelMask)

	m.items[idx].bucketIdx = bucket

	// Add to head
	head := m.buckets[bucket]
	m.items[idx].next = head
	m.items[idx].prev = -1

	if head != -1 {
		m.items[head].prev = idx
	}
	m.buckets[bucket] = idx
}

func (m *Map[V]) removeFromWheel(idx int) {
	item := &m.items[idx]
	bucket := item.bucketIdx
	if bucket == -1 {
		return
	}

	prev := item.prev
	next := item.next

	if prev != -1 {
		m.items[prev].next = next
	} else {
		// Head of list
		m.buckets[bucket] = next
	}

	if next != -1 {
		m.items[next].prev = prev
	}

	item.bucketIdx = -1
	item.prev = -1
	item.next = -1
}

// EvictExpired checks the bucket corresponding to 'now' and removes expired items.
// It calls cb for each expired item.
func (m *Map[V]) EvictExpired(now int64, cb func(string, V)) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	bucket := int((now / 1e9) & wheelMask)
	idx := m.buckets[bucket]

	var count uint64

	for idx != -1 {
		// Save next since we might delete current
		nextIdx := m.items[idx].next

		item := &m.items[idx]

		// Verify strict expiration (handle collisions in wheel bucket)
		if item.expire > 0 && item.expire <= now {
			// Expired!
			key := item.key
			val := item.val

			// Remove from lookup
			delete(m.lookup, key)

			// Remove from wheel
			m.removeFromWheel(idx)

			// Add to free list
			var zeroV V
			item.val = zeroV
			item.key = ""
			item.expire = 0
			m.free = append(m.free, idx)

			count++
			if cb != nil {
				cb(key, val)
			}
		}

		idx = nextIdx
	}

	return count
}
