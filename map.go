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
	"hash/maphash"
	"reflect"
	"unsafe"
)

// Map is a concurrent map with the sync.Map API plus typed helpers, backed by a
// single shardMapOf[K, *V, struct{}, struct{}] (nodes without valPtr and
// expire, see valueOf). Values are held as *V, so the Pointer variants keep
// pointer identity, and entries never expire. The zero Map is empty and ready
// to use; a Map must not be copied after first use.
type Map[K comparable, V any] struct {
	m shardMapOf[K, *V, struct{}, struct{}]
}

// LoadPointer returns the pointer stored for key.
func (m *Map[K, V]) LoadPointer(key K) (value *V, ok bool) {
	if m.m.empty() {
		return nil, false
	}
	value, _, ok = m.m.loadValue(maphash.Comparable(hashSeed, key), key)
	return value, ok
}

// Load returns the value stored for key.
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	p, ok := m.LoadPointer(key)
	if !ok {
		return value, false
	}
	return *p, true
}

// Store sets the value for key.
func (m *Map[K, V]) Store(key K, value V) {
	m.StorePointer(key, &value)
}

// StorePointer sets the pointer for key. A nil value deletes key.
func (m *Map[K, V]) StorePointer(key K, value *V) {
	if value == nil {
		m.LoadAndDeletePointer(key)
		return
	}
	m.m.store(maphash.Comparable(hashSeed, key), key, value, 0)
}

// Clear removes every entry.
func (m *Map[K, V]) Clear() {
	m.m.clear()
}

// LoadOrStore returns the existing value for key if present, otherwise it
// stores and returns value. loaded is true if the value was already present.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	val, loaded := m.LoadOrStorePointer(key, &value)
	return *val, loaded
}

// SwapPointer stores value for key and returns the previous pointer. A nil
// value deletes key (like LoadAndDeletePointer).
func (m *Map[K, V]) SwapPointer(key K, value *V) (previous *V, loaded bool) {
	if value == nil {
		return m.LoadAndDeletePointer(key)
	}
	return m.m.upsert(maphash.Comparable(hashSeed, key), key, value, 0, true)
}

// LoadOrStorePointer returns the existing pointer for key if present,
// otherwise it stores and returns value. A nil value on an absent key stores
// nothing.
func (m *Map[K, V]) LoadOrStorePointer(key K, value *V) (actual *V, loaded bool) {
	if value == nil {
		return m.LoadPointer(key)
	}
	return m.m.loadOrStore(maphash.Comparable(hashSeed, key), key, value, 0)
}

// LoadAndDelete deletes key and returns its previous value.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.LoadAndDeletePointer(key)
	if !loaded {
		return value, false
	}
	return *v, true
}

// LoadAndDeletePointer deletes key and returns its previous pointer.
func (m *Map[K, V]) LoadAndDeletePointer(key K) (value *V, loaded bool) {
	if m.m.empty() {
		return nil, false
	}
	return m.m.loadAndDelete(maphash.Comparable(hashSeed, key), key)
}

// Delete removes key.
func (m *Map[K, V]) Delete(key K) {
	m.LoadAndDeletePointer(key)
}

// Swap stores value for key and returns the previous value.
func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	v, loaded := m.SwapPointer(key, &value)
	if !loaded {
		return previous, false
	}
	return *v, true
}

// CompareAndSwap replaces the value for key with new if the current value is
// reflect.DeepEqual to old.
func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	n, ok := m.m.loadNode(maphash.Comparable(hashSeed, key), key)
	if !ok {
		return false
	}
	if p, _, live := n.readValue(); !live || !reflect.DeepEqual(*p, old) {
		return false
	}
	return n.updateIf(func(p *V) bool { return reflect.DeepEqual(*p, old) },
		func() *V { nc := new; return &nc })
}

// CompareAndSwapPointer replaces the pointer for key with new if the current
// pointer is old (identity, not content). A nil new deletes key.
func (m *Map[K, V]) CompareAndSwapPointer(key K, old, new *V) (swapped bool) {
	h := maphash.Comparable(hashSeed, key)
	if new == nil {
		_, _, swapped = m.m.popIf(h, key, func(p *V) bool { return p == old })
		return swapped
	}
	n, ok := m.m.loadNode(h, key)
	if !ok {
		return false
	}
	if p, _, live := n.readValue(); !live || p != old {
		return false
	}
	return n.updateIf(func(p *V) bool { return p == old }, func() *V { return new })
}

// CompareAndDelete deletes key if the current value is reflect.DeepEqual to old.
func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	_, _, deleted = m.m.popIf(maphash.Comparable(hashSeed, key), key, func(p *V) bool {
		return reflect.DeepEqual(*p, old)
	})
	return deleted
}

// Range calls f for every entry; it stops when f returns false. It walks the
// chains itself, like RangePointer: going through RangePointer adds a closure call
// per entry, +7.4% on BenchmarkMap_Range (benchstat, interleaved A/B, 2026-11-09,
// mission gache-critical-path-slim).
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	for _, head := range m.m.readSnapshot().m {
		for n := head; n != nil; n = loadNext(n) {
			if p, ok := n.loadLive(); ok && !f(n.key, *p) {
				return
			}
		}
	}
}

// RangePointer is Range with pointer values.
func (m *Map[K, V]) RangePointer(f func(key K, value *V) bool) {
	for _, head := range m.m.readSnapshot().m {
		for n := head; n != nil; n = loadNext(n) {
			if p, ok := n.loadLive(); ok && !f(n.key, p) {
				return
			}
		}
	}
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int {
	return m.m.length()
}

// Size estimates the bytes owned by the map: its internal structures, its
// nodes (each with the key as stored), and the boxed value of every live entry,
// each rounded up the way the Go allocator rounds a heap object. It excludes the
// bytes the keys and the values point to (such as string data). It is an
// estimate: a value stored with StorePointer is charged as one boxed V even when
// it is shared or part of a larger object, and the tiny allocator can keep a
// deleted small value's block alive. It takes the shard lock briefly and
// allocates nothing.
func (m *Map[K, V]) Size() uintptr {
	if m == nil {
		return 0
	}
	// unsafe.Sizeof(V) is constant per instantiation, so scanMatters folds and
	// typeHasPointers is read only for a V whose rounding depends on it.
	vs := unsafe.Sizeof(*new(V))
	return m.m.size() + uintptr(m.Len())*allocSize(vs, scanMatters(vs) && typeHasPointers[V]())
}

// InitReserve pre-sizes an empty map for size entries.
func (m *Map[K, V]) InitReserve(size int) {
	m.m.initReserve(size)
}
