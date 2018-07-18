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
		DeleteExpired(ctx context.Context) <-chan int
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

// New returns Gache (*gache) instance
func New() Gache {
	return newGache()
}

// newGache returns *gache instance
func newGache() *gache {
	g := &gache{
		data:   new(sync.Map),
		expire: new(atomic.Value),
	}
	g.expire.Store(time.Second * 30)
	return g
}

// GetGache returns Gache (*gache) instance
func GetGache() Gache {
	once.Do(func() {
		instance = newGache()
	})
	return instance
}

// isValid checks expiration of value
func (v *value) isValid() bool {
	return v.expire == 0 || time.Now().UnixNano() < v.expire
}

// SetDefaultExpire set expire duration
func (g *gache) SetDefaultExpire(ex time.Duration) Gache {
	g.expire.Store(ex)
	return g
}

// SetDefaultExpire set expire duration
func SetDefaultExpire(ex time.Duration) {
	instance.SetDefaultExpire(ex)
}

// StartExpired starts delete expired value daemon
func (g *gache) StartExpired(ctx context.Context, dur time.Duration) Gache {
	go func() {
		tick := time.NewTicker(dur)
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case <-tick.C:
				g.DeleteExpired(ctx)
			}
		}
	}()
	return g
}

// ToMap returns All Cache Key-Value map
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

// ToMap returns All Cache Key-Value map
func ToMap() map[interface{}]interface{} {
	return instance.ToMap()
}

// get returns value & exists from key
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

// Get returns value & exists from key
func (g *gache) Get(key interface{}) (interface{}, bool) {
	return g.get(key)
}

// Get returns value & exists from key
func Get(key interface{}) (interface{}, bool) {
	return instance.get(key)
}

// set sets key-value & expiration to Gache
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

// SetWithExpire sets key-value & expiration to Gache
func (g *gache) SetWithExpire(key, val interface{}, expire time.Duration) {
	g.set(key, val, expire)
}

// SetWithExpire sets key-value & expiration to Gache
func SetWithExpire(key, val interface{}, expire time.Duration) {
	instance.set(key, val, expire)
}

// Set sets key-value to Gache using default expiration
func (g *gache) Set(key, val interface{}) {
	g.set(key, val, g.expire.Load().(time.Duration))
}

// Set sets key-value to Gache using default expiration
func Set(key, val interface{}) {
	instance.set(key, val, instance.expire.Load().(time.Duration))
}

// Delete deletes value from Gache using key
func (g *gache) Delete(key interface{}) {
	g.data.Delete(key)
}

// Delete deletes value from Gache using key
func Delete(key interface{}) {
	instance.Delete(key)
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func (g *gache) DeleteExpired(ctx context.Context) <-chan int {
	c := make(chan int)
	go func() {
		var rows int
		g.data.Range(func(k, v interface{}) bool {
			select {
			case <-ctx.Done():
				return false
			default:
				d, ok := v.(*value)
				if ok && !d.isValid() {
					g.data.Delete(k)
					rows++
				}
				return true
			}
		})
		c <- rows
	}()
	return c
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func DeleteExpired(ctx context.Context) <-chan int {
	return instance.DeleteExpired(ctx)
}

// Foreach calls f sequentially for each key and value present in the Gache.
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

// Foreach calls f sequentially for each key and value present in the Gache.
func Foreach(f func(interface{}, interface{}, int64) bool) Gache {
	return instance.Foreach(f)
}

// Clear deletes all key and value present in the Gache.
func (g *gache) Clear() {
	g.data.Range(func(key, _ interface{}) bool {
		g.data.Delete(key)
		return true
	})
	g.data = new(sync.Map)
}

// Clear deletes all key and value present in the Gache.
func Clear() {
	instance.Clear()
}
