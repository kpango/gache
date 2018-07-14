package gache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type (
	// Gache is base interface type
	Gache interface {
		Clear()
		Delete(interface{})
		DeleteExpired() int
		Foreach(func(interface{}, interface{}, int64) bool) Gache
		Get(interface{}) (interface{}, bool)
		Set(interface{}, interface{})
		SetDefaultExpire(time.Duration) Gache
		SetWithExpire(interface{}, interface{}, time.Duration)
		StartExpired(context.Context, time.Duration) Gache
		ToMap() map[interface{}]interface{}
	}

	// gache is base instance type
	gache struct {
		data   *sync.Map
		expire *atomic.Value
	}

	value struct {
		expire int64
		val    *interface{}
	}
)

var (
	instance *gache
	once     sync.Once
)

func init() {
	GetGache()
}

func New() Gache {
	return newGache()
}

func newGache() *gache {
	g := &gache{
		data:   new(sync.Map),
		expire: new(atomic.Value),
	}
	g.expire.Store(time.Second * 30)
	return g
}

func GetGache() Gache {
	once.Do(func() {
		instance = newGache()
	})
	return instance
}

func (v *value) isValid() bool {
	return v.expire == 0 || time.Now().UnixNano() < v.expire
}

func SetDefaultExpire(ex time.Duration) {
	instance.SetDefaultExpire(ex)
}

func (g *gache) SetDefaultExpire(ex time.Duration) Gache {
	g.expire.Store(ex)
	return g
}

func (g *gache) StartExpired(ctx context.Context, dur time.Duration) Gache {
	go func() {
		tick := time.NewTicker(dur)
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				g.DeleteExpired()
			}
		}
	}()
	return g
}

func ToMap() map[interface{}]interface{} {
	return instance.ToMap()
}

func (g *gache) ToMap() map[interface{}]interface{} {
	m := make(map[interface{}]interface{})
	g.data.Range(func(k, v interface{}) bool {
		d, ok := v.(*value)
		if ok {
			if d.isValid() {
				m[k] = *d.val
			} else {
				g.Delete(k)
			}
			return true
		}
		return false
	})
	return m
}

func Get(key interface{}) (interface{}, bool) {
	return instance.get(key)
}

func (g *gache) Get(key interface{}) (interface{}, bool) {
	return g.get(key)
}

func (g *gache) get(key interface{}) (interface{}, bool) {

	v, ok := g.data.Load(key)

	if !ok {
		return nil, false
	}

	d, ok := v.(*value)

	if !ok || !d.isValid() {
		g.data.Delete(key)
		return nil, false
	}

	return *d.val, true
}

func SetWithExpire(key, val interface{}, expire time.Duration) {
	instance.set(key, val, expire)
}

func Set(key, val interface{}) {
	instance.set(key, val, instance.expire.Load().(time.Duration))
}

func (g *gache) SetWithExpire(key, val interface{}, expire time.Duration) {
	g.set(key, val, expire)
}

func (g *gache) Set(key, val interface{}) {
	g.set(key, val, g.expire.Load().(time.Duration))
}

func (g *gache) set(key, val interface{}, expire time.Duration) {
	var exp int64
	if expire != 0 {
		exp = time.Now().Add(expire).UnixNano()
	}
	g.data.Store(key, &value{
		expire: exp,
		val:    &val,
	})
}

func (g *gache) DeleteExpired() int {
	var rows int
	g.data.Range(func(k, v interface{}) bool {
		d, ok := v.(*value)
		if ok && !d.isValid() {
			g.data.Delete(k)
			rows++
		}
		return true
	})
	return rows
}

func Delete(key interface{}) {
	instance.Delete(key)
}

func (g *gache) Delete(key interface{}) {
	g.data.Delete(key)
}

func Foreach(f func(interface{}, interface{}, int64) bool) Gache {
	return instance.Foreach(f)
}

func (g *gache) Foreach(f func(interface{}, interface{}, int64) bool) Gache {
	g.data.Range(func(k, v interface{}) bool {
		d, ok := v.(*value)
		if ok {
			if d.isValid() {
				return f(k, *d.val, d.expire)
			} else {
				g.Delete(k)
			}
		}
		return false
	})
	return g
}

func (g *gache) Clear() {
	g.data.Range(func(key, _ interface{}) bool {
		g.data.Delete(key)
		return true
	})
	g.data = new(sync.Map)
}

func Clear() {
	instance.Clear()
}
