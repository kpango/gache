// Copyright (c) 2009 The Go Authors. All rights resered.
// Modified <Yusuke Kato (kpango)>

// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:

//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.

// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package gache

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Map is a concurrent map optimized for write-heavy workloads.
// It uses a flat Go map protected by a sync.RWMutex and stores values
// directly (value semantics) to eliminate pointer chasing and reduce GC
// pressure. Cache-line padding prevents false sharing when multiple Map
// instances are laid out adjacently in an array (e.g. the gache shard array).
type Map[K comparable, V any] struct {
	_    [128]byte // cache-line padding (front) — isolates this shard from its predecessor
	mu   sync.RWMutex
	data map[K]V
	l    atomic.Uint64
	_    [128]byte // cache-line padding (rear) — isolates this shard from its successor
}

func (m *Map[K, V]) init() {
	if m.data == nil {
		m.data = make(map[K]V)
	}
}

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	m.mu.RLock()
	if m.data != nil {
		value, ok = m.data[key]
	}
	m.mu.RUnlock()
	return value, ok
}

func (m *Map[K, V]) Store(key K, value V) {
	m.mu.Lock()
	m.init()
	_, exists := m.data[key]
	m.data[key] = value
	if !exists {
		m.l.Add(1)
	}
	m.mu.Unlock()
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	m.mu.Lock()
	m.init()
	actual, loaded = m.data[key]
	if !loaded {
		m.data[key] = value
		actual = value
		m.l.Add(1)
	}
	m.mu.Unlock()
	return actual, loaded
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	m.mu.Lock()
	if m.data != nil {
		value, loaded = m.data[key]
		if loaded {
			delete(m.data, key)
			m.l.Add(^uint64(0))
		}
	}
	m.mu.Unlock()
	return value, loaded
}

func (m *Map[K, V]) Delete(key K) {
	m.LoadAndDelete(key)
}

func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	m.mu.Lock()
	m.init()
	previous, loaded = m.data[key]
	m.data[key] = value
	if !loaded {
		m.l.Add(1)
	}
	m.mu.Unlock()
	return previous, loaded
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	m.mu.Lock()
	if m.data != nil {
		if current, ok := m.data[key]; ok && reflect.DeepEqual(current, old) {
			m.data[key] = new
			swapped = true
		}
	}
	m.mu.Unlock()
	return swapped
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	m.mu.Lock()
	if m.data != nil {
		if current, ok := m.data[key]; ok && reflect.DeepEqual(current, old) {
			delete(m.data, key)
			m.l.Add(^uint64(0))
			deleted = true
		}
	}
	m.mu.Unlock()
	return deleted
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, Range stops the iteration.
//
// Range snapshots the keys under a read lock, then iterates without holding
// the lock so that f may safely call other Map methods (e.g. Delete) without
// deadlocking.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.mu.RLock()
	keys := make([]K, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	m.mu.RUnlock()

	for _, k := range keys {
		v, ok := m.Load(k)
		if !ok {
			continue
		}
		if !f(k, v) {
			break
		}
	}
}

// StoreIfPresent atomically stores value for key only if the key already exists.
// Returns true if the store was performed.
func (m *Map[K, V]) StoreIfPresent(key K, value V) (stored bool) {
	m.mu.Lock()
	if m.data != nil {
		if _, exists := m.data[key]; exists {
			m.data[key] = value
			stored = true
		}
	}
	m.mu.Unlock()
	return stored
}

func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	clear(m.data)
	m.l.Store(0)
	m.mu.Unlock()
}

func (m *Map[K, V]) Len() int {
	return int(m.l.Load())
}

func (m *Map[K, V]) Size() (size uintptr) {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	size = unsafe.Sizeof(*m)
	size += mapSize(m.data)
	return size
}
