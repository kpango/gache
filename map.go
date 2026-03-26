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
	"unsafe"

	"reflect"
	"sync"
	"sync/atomic"
)

var expungedGlobal atomic.Int64

func expungedPtr[V any]() *V {
	return (*V)(unsafe.Pointer(&expungedGlobal))
}

type Map[K comparable, V any] struct {
	read   atomic.Pointer[readOnly[K, V]]
	dirty  map[K]*entry[V]
	l      atomic.Pointer[atomic.Int64]
	misses int
	mu     sync.RWMutex
}

type readOnly[K comparable, V any] struct {
	m       map[K]*entry[V]
	amended bool
}

type entry[V any] struct {
	p atomic.Pointer[V]
	c *atomic.Int64
}

func newEntryPointer[V any](v *V, c *atomic.Int64) (e *entry[V]) {
	e = &entry[V]{}
	e.p.Store(v)
	e.c = c
	return e
}

func (e *entry[V]) isExpunged(p *V) bool {
	return p == expungedPtr[V]()
}

func (m *Map[K, V]) counter() *atomic.Int64 {
	c := m.l.Load()
	if c != nil {
		return c
	}
	c = new(atomic.Int64)
	if m.l.CompareAndSwap(nil, c) {
		return c
	}
	return m.l.Load()
}

func (m *Map[K, V]) loadReadOnly() (ro readOnly[K, V]) {
	if p := m.read.Load(); p != nil {
		return *p
	}
	return readOnly[K, V]{}
}

// loadEntry abstracts the double-checked locking mechanism to find an entry.
// If del is true, it deletes the entry from the dirty map if it is found there.
func (m *Map[K, V]) loadEntry(key K, del bool) (*entry[V], bool) {
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok || !read.amended {
		return e, ok
	}
	return m.loadEntrySlow(key, del)
}

func (m *Map[K, V]) loadEntrySlow(key K, del bool) (*entry[V], bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	e, ok := read.m[key]
	if !ok && read.amended {
		e, ok = m.dirty[key]
		if ok && del {
			delete(m.dirty, key)
		}
		m.missLocked()
	}
	return e, ok
}

func (m *Map[K, V]) LoadPointer(key K) (value *V, ok bool) {
	read := m.loadReadOnly()
	e, ok := read.m[key]
	if !ok && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		e, ok = read.m[key]
		if !ok && read.amended {
			e, ok = m.dirty[key]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	if !ok {
		return nil, false
	}
	return e.loadPointer()
}

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	p, ok := m.LoadPointer(key)
	if !ok || p == nil {
		return value, false
	}
	return *p, true
}

func (e *entry[V]) load() (value V, ok bool) {
	p := e.p.Load()
	if p == nil || e.isExpunged(p) {
		return value, false
	}
	return *p, true
}

func (e *entry[V]) loadPointer() (value *V, ok bool) {
	p := e.p.Load()
	if p == nil || e.isExpunged(p) {
		return nil, false
	}
	return p, true
}

func (m *Map[K, V]) Store(key K, value V) {
	m.SwapPointer(key, &value)
}

func (m *Map[K, V]) StorePointer(key K, value *V) {
	m.SwapPointer(key, value)
}

func (m *Map[K, V]) Clear() {
	read := m.loadReadOnly()
	if len(read.m) == 0 && !read.amended {
		// Avoid allocating a new readOnly when the map is already clear.
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	read = m.loadReadOnly()
	if len(read.m) > 0 || read.amended {
		m.read.Store(&readOnly[K, V]{})
	}

	clear(m.dirty)
	// Don't immediately promote the newly-cleared dirty map on the next operation.
	m.misses = 0
	m.l.Store(new(atomic.Int64))
}

func (e *entry[V]) unexpungeLocked() (wasExpunged bool) {
	return e.p.CompareAndSwap(expungedPtr[V](), nil)
}

func (e *entry[V]) swapLocked(i *V) (v *V) {
	return e.p.Swap(i)
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	val, loaded := m.LoadOrStorePointer(key, &value)
	if val != nil {
		return *val, loaded
	}
	return actual, loaded
}

func (m *Map[K, V]) SwapPointer(key K, value *V) (previous *V, loaded bool) {
	if value == nil {
		previous, loaded = m.LoadAndDeletePointer(key)
		return previous, loaded
	}
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		if v, ok := e.trySwap(value); ok {
			if v == nil {
				e.c.Add(1)
				return nil, false
			}
			return v, true
		}
	}
	previous, loaded = m.swapPointerSlow(key, value)
	return previous, loaded
}

func (m *Map[K, V]) swapPointerSlow(key K, value *V) (previous *V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		if e.unexpungeLocked() {
			if m.dirty == nil {
				m.initDirty(len(read.m))
			}
			m.dirty[key] = e
		}
		if v := e.swapLocked(value); v != nil {
			loaded = true
			previous = v
		} else {
			e.c.Add(1)
		}
	} else if e, ok := m.dirty[key]; ok {
		if v := e.swapLocked(value); v != nil {
			loaded = true
			previous = v
		} else {
			e.c.Add(1)
		}
	} else {
		if !read.amended {
			m.dirtyLocked()
			m.read.Store(&readOnly[K, V]{m: read.m, amended: true})
		}
		if m.dirty == nil {
			m.initDirty(len(read.m))
		}
		ne := newEntryPointer(value, m.counter())
		m.dirty[key] = ne
		ne.c.Add(1)
	}
	return previous, loaded
}

func (m *Map[K, V]) LoadOrStorePointer(key K, value *V) (actual *V, loaded bool) {
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		actual, loaded, ok := e.tryLoadOrStorePointer(value)
		if ok {
			if !loaded {
				e.c.Add(1)
			}
			return actual, loaded
		}
	}
	actual, loaded = m.loadOrStorePointerSlow(key, value)
	return actual, loaded
}

func (m *Map[K, V]) loadOrStorePointerSlow(key K, value *V) (actual *V, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		if e.unexpungeLocked() {
			if m.dirty == nil {
				m.initDirty(len(read.m))
			}
			m.dirty[key] = e
		}
		actual, loaded, _ = e.tryLoadOrStorePointer(value)
		if !loaded {
			e.c.Add(1)
		}
	} else if e, ok := m.dirty[key]; ok {
		actual, loaded, _ = e.tryLoadOrStorePointer(value)
		if !loaded {
			e.c.Add(1)
		}
		m.missLocked()
	} else {
		if !read.amended {
			m.dirtyLocked()
			m.read.Store(&readOnly[K, V]{m: read.m, amended: true})
		}
		if m.dirty == nil {
			m.initDirty(len(read.m))
		}
		ne := newEntryPointer(value, m.counter())
		m.dirty[key] = ne
		actual, loaded = ne.p.Load(), false
		ne.c.Add(1)
	}
	return actual, loaded
}

func (e *entry[V]) tryLoadOrStorePointer(i *V) (actual *V, loaded, ok bool) {
	p := e.p.Load()
	if e.isExpunged(p) {
		return nil, false, false
	}
	if p != nil {
		return p, true, true
	}

	for {
		if e.p.CompareAndSwap(nil, i) {
			return i, false, true
		}
		p = e.p.Load()
		if e.isExpunged(p) {
			return nil, false, false
		}
		if p != nil {
			return p, true, true
		}
	}
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.LoadAndDeletePointer(key)
	if !loaded {
		return value, false
	}
	return *v, true
}

func (m *Map[K, V]) LoadAndDeletePointer(key K) (value *V, loaded bool) {
	e, ok := m.loadEntry(key, true)
	if ok && e != nil {
		value, loaded = e.deletePointer()
		if loaded {
			e.c.Add(-1)
		}
		return value, loaded
	}
	return nil, false
}

func (e *entry[V]) deletePointer() (value *V, ok bool) {
	for {
		p := e.p.Load()
		if p == nil || e.isExpunged(p) {
			return nil, false
		}
		if e.p.CompareAndSwap(p, nil) {
			return p, true
		}
	}
}

func (m *Map[K, V]) Delete(key K) {
	m.LoadAndDeletePointer(key)
}

func (e *entry[V]) trySwap(i *V) (v *V, ok bool) {
	for {
		p := e.p.Load()
		if e.isExpunged(p) {
			return nil, false
		}
		if e.p.CompareAndSwap(p, i) {
			return p, true
		}
	}
}

func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	v, loaded := m.SwapPointer(key, &value)
	if !loaded || v == nil {
		return previous, loaded
	}
	return *v, loaded
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		return e.tryCompareAndSwap(&old, new)
	} else if !read.amended {
		return false
	}
	return m.casEntrySlow(key, func(e *entry[V]) bool {
		return e.tryCompareAndSwap(&old, new)
	})
}

func (m *Map[K, V]) casEntrySlow(key K, tryCAS func(*entry[V]) bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		return tryCAS(e)
	} else if e, ok := m.dirty[key]; ok {
		swapped := tryCAS(e)
		m.missLocked()
		return swapped
	}
	return false
}

func (m *Map[K, V]) CompareAndSwapPointer(key K, old, new *V) (swapped bool) {
	read := m.loadReadOnly()
	if e, ok := read.m[key]; ok {
		return e.tryCompareAndSwapPointer(old, new)
	} else if !read.amended {
		return false
	}
	return m.casEntrySlow(key, func(e *entry[V]) bool {
		return e.tryCompareAndSwapPointer(old, new)
	})
}

func (e *entry[V]) tryCompareAndSwap(oldp *V, new V) (ok bool) {
	p := e.p.Load()
	if p == nil || e.isExpunged(p) || !reflect.DeepEqual(*p, *oldp) {
		return false
	}

	nc := new
	for {
		if e.p.CompareAndSwap(p, &nc) {
			return true
		}
		p = e.p.Load()
		if p == nil || e.isExpunged(p) || !reflect.DeepEqual(*p, *oldp) {
			return false
		}
	}
}

func (e *entry[V]) tryCompareAndSwapPointer(old, new *V) (ok bool) {
	p := e.p.Load()
	if e.isExpunged(p) {
		return false
	}
	if p != old {
		return false
	}
	return e.p.CompareAndSwap(old, new)
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	read := m.loadReadOnly()
	e, ok := read.m[key]
	if !ok && read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		e, ok = read.m[key]
		if !ok && read.amended {
			e, ok = m.dirty[key]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	for ok {
		p := e.p.Load()
		if p == nil || e.isExpunged(p) || !reflect.DeepEqual(*p, old) {
			return false
		}
		if e.p.CompareAndSwap(p, nil) {
			e.c.Add(-1)
			return true
		}
	}
	return false
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.RangePointer(func(key K, value *V) bool {
		return f(key, *value)
	})
}

func (m *Map[K, V]) RangePointer(f func(key K, value *V) bool) {
	read := m.loadReadOnly()
	if read.amended {
		m.mu.Lock()
		read = m.loadReadOnly()
		if read.amended {
			read = readOnly[K, V]{m: m.dirty}
			copyRead := read
			m.read.Store(&copyRead)
			m.dirty = nil
			m.misses = 0
		}
		m.mu.Unlock()
	}

	for k, e := range read.m {
		v, ok := e.loadPointer()
		if !ok {
			continue
		}
		if !f(k, v) {
			break
		}
	}
}

func (m *Map[K, V]) missLocked() {
	m.misses++
	if m.misses < len(m.dirty) {
		return
	}
	m.read.Store(&readOnly[K, V]{m: m.dirty})
	m.dirty = nil
	m.misses = 0
}

func (m *Map[K, V]) dirtyLocked() {
	if m.dirty != nil {
		return
	}

	read := m.loadReadOnly()
	m.initDirty(len(read.m))
	for k, e := range read.m {
		if !e.tryExpungeLocked() {
			m.dirty[k] = e
		}
	}
}

func (m *Map[K, V]) initDirty(size int) {
	m.dirty = make(map[K]*entry[V], size)
}

func (e *entry[V]) tryExpungeLocked() (isExpunged bool) {
	p := e.p.Load()
	for p == nil {
		if e.p.CompareAndSwap(nil, expungedPtr[V]()) {
			return true
		}
		p = e.p.Load()
	}
	return e.isExpunged(p)
}

func (m *Map[K, V]) Len() int {
	c := m.counter()
	l := int(c.Load())
	if l < 0 {
		return 0
	}
	return l
}

func (m *Map[K, V]) Size() (size uintptr) {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	size = unsafe.Sizeof(*m) // Includes mu, read, dirty, misses, l

	if m.l.Load() != nil {
		size += unsafe.Sizeof(atomic.Int64{})
	}

	if ro := m.read.Load(); ro != nil {
		size += ro.Size()
	}
	size += mapSize(m.dirty)
	if l := len(m.dirty); l > 0 {
		size += uintptr(l) * (unsafe.Sizeof(entry[V]{}) + unsafe.Sizeof(*new(V)))
	}
	return size
}

func (e *entry[V]) Size() (size uintptr) {
	if e == nil {
		return 0
	}
	size += unsafe.Sizeof(e.p)

	if ep := e.p.Load(); ep != nil && !e.isExpunged(ep) {
		size += unsafe.Sizeof(*ep)
	}
	return size
}

func (r readOnly[K, V]) Size() (size uintptr) {
	size = unsafe.Sizeof(r.amended)
	size += mapSize(r.m)
	if l := len(r.m); l > 0 {
		size += uintptr(l) * (unsafe.Sizeof(entry[V]{}) + unsafe.Sizeof(*new(V)))
	}
	return size
}

func (m *Map[K, V]) InitReserve(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirty == nil && len(m.loadReadOnly().m) == 0 {
		m.initDirty(size)
	}
}
