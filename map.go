package gache

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

type Map struct {
	mu     sync.Mutex
	read   atomic.Value
	dirty  map[string]*entryMap
	misses int
}

type readOnlyMap struct {
	m       map[string]*entryMap
	amended bool
}

var expungedMap = unsafe.Pointer(new(value))

type entryMap struct {
	p unsafe.Pointer
}

func newEntryMap(i value) *entryMap {
	return &entryMap{p: unsafe.Pointer(&i)}
}

func (m *Map) Load(key string) (value value, ok bool) {
	read, _ := m.read.Load().(readOnlyMap)
	e, ok := read.m[key]
	if !ok && read.amended {
		m.mu.Lock()
		read, _ = m.read.Load().(readOnlyMap)
		e, ok = read.m[key]
		if !ok && read.amended {
			e, ok = m.dirty[key]
			m.missLocked()
		}
		m.mu.Unlock()
	}
	if !ok {
		return value, false
	}
	return e.load()
}

func (e *entryMap) load() (val value, ok bool) {
	p := atomic.LoadPointer(&e.p)
	if p == nil || p == expungedMap {
		return val, false
	}
	return *(*value)(p), true
}

func (m *Map) Store(key string, value value) {
	read, _ := m.read.Load().(readOnlyMap)
	if e, ok := read.m[key]; ok && e.tryStore(&value) {
		return
	}

	m.mu.Lock()
	read, _ = m.read.Load().(readOnlyMap)
	if e, ok := read.m[key]; ok {
		if e.unexpungeLocked() {
			m.dirty[key] = e
		}
		e.storeLocked(&value)
	} else if e, ok := m.dirty[key]; ok {
		e.storeLocked(&value)
	} else {
		if !read.amended {
			m.dirtyLocked()
			m.read.Store(readOnlyMap{m: read.m, amended: true})
		}
		m.dirty[key] = newEntryMap(value)
	}
	m.mu.Unlock()
}

func (e *entryMap) tryStore(i *value) bool {
	for {
		p := atomic.LoadPointer(&e.p)
		if p == expungedMap {
			return false
		}
		if atomic.CompareAndSwapPointer(&e.p, p, unsafe.Pointer(i)) {
			return true
		}
	}
}

func (e *entryMap) unexpungeLocked() (wasExpunged bool) {
	return atomic.CompareAndSwapPointer(&e.p, expungedMap, nil)
}

func (e *entryMap) storeLocked(i *value) {
	atomic.StorePointer(&e.p, unsafe.Pointer(i))
}

func (m *Map) LoadAndDelete(key string) (value value, loaded bool) {
	read, _ := m.read.Load().(readOnlyMap)
	e, ok := read.m[key]
	if !ok && read.amended {
		m.mu.Lock()
		read, _ = m.read.Load().(readOnlyMap)
		e, ok = read.m[key]
		if !ok && read.amended {
			e, ok = m.dirty[key]
			delete(m.dirty, key)
			m.missLocked()
		}
		m.mu.Unlock()
	}
	if ok {
		return e.delete()
	}
	return value, false
}

func (m *Map) Delete(key string) {
	m.LoadAndDelete(key)
}

func (e *entryMap) delete() (val value, ok bool) {
	for {
		p := atomic.LoadPointer(&e.p)
		if p == nil || p == expungedMap {
			return val, false
		}
		if atomic.CompareAndSwapPointer(&e.p, p, nil) {
			return *(*value)(p), true
		}
	}
}

func (m *Map) Range(f func(key string, value value) bool) {
	read, _ := m.read.Load().(readOnlyMap)
	if read.amended {
		m.mu.Lock()
		read, _ = m.read.Load().(readOnlyMap)
		if read.amended {
			read = readOnlyMap{m: m.dirty}
			m.read.Store(read)
			m.dirty = nil
			m.misses = 0
		}
		m.mu.Unlock()
	}

	for k, e := range read.m {
		v, ok := e.load()
		if !ok {
			continue
		}
		if !f(k, v) {
			break
		}
	}
}

func (m *Map) missLocked() {
	m.misses++
	if m.misses < len(m.dirty) {
		return
	}
	m.read.Store(readOnlyMap{m: m.dirty})
	m.dirty = nil
	m.misses = 0
}

func (m *Map) dirtyLocked() {
	if m.dirty != nil {
		return
	}

	read, _ := m.read.Load().(readOnlyMap)
	m.dirty = make(map[string]*entryMap, len(read.m))
	for k, e := range read.m {
		if !e.tryExpungeLocked() {
			m.dirty[k] = e
		}
	}
}

func (e *entryMap) tryExpungeLocked() (isExpunged bool) {
	p := atomic.LoadPointer(&e.p)
	for p == nil {
		if atomic.CompareAndSwapPointer(&e.p, nil, expungedMap) {
			return true
		}
		p = atomic.LoadPointer(&e.p)
	}
	return p == expungedMap
}
