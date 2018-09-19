package gache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash"
	"github.com/kpango/fastime"
	"golang.org/x/sync/singleflight"
)

type (
	// Gache is base interface type
	Gache interface {
		Clear()
		Delete(string)
		DeleteExpired(ctx context.Context) <-chan uint64
		Foreach(context.Context, func(string, interface{}, int64) bool) Gache
		Get(string) (interface{}, bool)
		Set(string, interface{})
		SetDefaultExpire(time.Duration) Gache
		SetExpiredHook(f func(context.Context, string)) Gache
		EnableExpiredHook() Gache
		DisableExpiredHook() Gache
		SetWithExpire(string, interface{}, time.Duration)
		StartExpired(context.Context, time.Duration) Gache
		ToMap(context.Context) *sync.Map
	}

	// gache is base instance type
	gache struct {
		l              uint64
		shards         [255]*shard
		expire         *atomic.Value
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
	}

	shard struct {
		data *sync.Map
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
		expire: new(atomic.Value),
	}
	g.l = uint64(len(g.shards))
	for i := range g.shards {
		g.shards[i] = &shard{data: new(sync.Map)}
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
	return v.expire == 0 || fastime.Now().UnixNano() < v.expire
}

// SetDefaultExpire set expire duration
func (g *gache) SetDefaultExpire(ex time.Duration) Gache {
	g.expire.Store(ex)
	return g
}

// SetDefaultExpire set expire duration
func SetDefaultExpire(ex time.Duration) Gache {
	return instance.SetDefaultExpire(ex)
}

// EnableExpiredHook enables expired hook function
func (g *gache) EnableExpiredHook() Gache {
	g.expFuncEnabled = true
	return g
}

// EnableExpiredHook enables expired hook function
func EnableExpiredHook() Gache {
	return instance.EnableExpiredHook()
}

// DisableExpiredHook disables expired hook function
func (g *gache) DisableExpiredHook() Gache {
	g.expFuncEnabled = false
	return g
}

// DisableExpiredHook disables expired hook function
func DisableExpiredHook() Gache {
	return instance.DisableExpiredHook()
}

// SetExpiredHook set expire hooked function
func (g *gache) SetExpiredHook(f func(context.Context, string)) Gache {
	g.expFunc = f
	return g
}

// SetExpiredHook set expire hooked function
func SetExpiredHook(f func(context.Context, string)) Gache {
	return instance.SetExpiredHook(f)
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
			case key := <-g.expChan:
				go g.expFunc(ctx, key)
			}
		}
	}()
	return g
}

// ToMap returns All Cache Key-Value map
func (g *gache) ToMap(ctx context.Context) *sync.Map {
	m := new(sync.Map)
	g.Foreach(ctx, func(key string, val interface{}, exp int64) bool {
		m.Store(key, val)
		return true
	})

	return m
}

// ToMap returns All Cache Key-Value map
func ToMap(ctx context.Context) *sync.Map {
	return instance.ToMap(ctx)
}

// get returns value & exists from key
func (g *gache) get(key string) (interface{}, bool) {
	shard := g.getShard(key)
	v, ok := shard.Load(key)

	if !ok {
		return nil, false
	}

	d, ok := v.(*value)

	if !ok || !d.isValid() {
		g.expiration(key)
		return nil, false
	}

	return *d.val, true
}

// Get returns value & exists from key
func (g *gache) Get(key string) (interface{}, bool) {
	return g.get(key)
}

// Get returns value & exists from key
func Get(key string) (interface{}, bool) {
	return instance.get(key)
}

// set sets key-value & expiration to Gache
func (g *gache) set(key string, val interface{}, expire time.Duration) {
	var exp int64
	if expire > 0 {
		exp = fastime.Now().Add(expire).UnixNano()
	}
	g.getShard(key).Store(key, &value{
		expire: exp,
		val:    &val,
	})
}

// SetWithExpire sets key-value & expiration to Gache
func (g *gache) SetWithExpire(key string, val interface{}, expire time.Duration) {
	g.set(key, val, expire)
}

// SetWithExpire sets key-value & expiration to Gache
func SetWithExpire(key string, val interface{}, expire time.Duration) {
	instance.set(key, val, expire)
}

// Set sets key-value to Gache using default expiration
func (g *gache) Set(key string, val interface{}) {
	g.set(key, val, g.expire.Load().(time.Duration))
}

// Set sets key-value to Gache using default expiration
func Set(key string, val interface{}) {
	instance.set(key, val, instance.expire.Load().(time.Duration))
}

// Delete deletes value from Gache using key
func (g *gache) Delete(key string) {
	g.getShard(key).Delete(key)
}

// Delete deletes value from Gache using key
func Delete(key string) {
	instance.Delete(key)
}

func (g *gache) expiration(key string) {
	g.expGroup.Do(key, func() (interface{}, error) {
		g.Delete(key)
		if g.expFuncEnabled {
			g.expChan <- key
		}
		return nil, nil
	})
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func (g *gache) DeleteExpired(ctx context.Context) <-chan uint64 {
	ch := make(chan uint64)
	go func() {
		wg := new(sync.WaitGroup)
		rows := new(uint64)
		for i := range g.shards {
			wg.Add(1)
			go func(c context.Context) {
				g.shards[i].data.Range(func(k, v interface{}) bool {
					select {
					case <-c.Done():
						return false
					default:
						d, ok := v.(*value)
						if ok && !d.isValid() {
							g.expiration(k.(string))
							atomic.AddUint64(rows, 1)
						}
						return false
					}
				})
				wg.Done()
			}(ctx)
		}
		wg.Wait()
		ch <- atomic.LoadUint64(rows)
	}()
	return ch
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func DeleteExpired(ctx context.Context) <-chan uint64 {
	return instance.DeleteExpired(ctx)
}

// Foreach calls f sequentially for each key and value present in the Gache.
func (g *gache) Foreach(ctx context.Context, f func(string, interface{}, int64) bool) Gache {
	wg := new(sync.WaitGroup)
	for _, shard := range g.shards {
		wg.Add(1)
		go func(c context.Context) {
			shard.data.Range(func(k, v interface{}) bool {
				select {
				case <-c.Done():
					return false
				default:
					d, ok := v.(*value)
					if ok {
						if d.isValid() {
							return f(k.(string), *d.val, d.expire)
						}
						g.expiration(k.(string))
					}
					return false
				}
			})
			wg.Done()
		}(ctx)
	}
	wg.Wait()
	return g
}

// Foreach calls f sequentially for each key and value present in the Gache.
func Foreach(ctx context.Context, f func(string, interface{}, int64) bool) Gache {
	return instance.Foreach(ctx, f)
}

func (g *gache) selectShard(key string) uint64 {
	return xxhash.Sum64String(key)
}

func (g *gache) getShard(key string) *sync.Map {
	return g.shards[g.selectShard(key)%g.l].data
}

// Clear deletes all key and value present in the Gache.
func (g *gache) Clear() {
	for i := range g.shards {
		g.shards[i] = &shard{data: new(sync.Map)}
	}
}

// Clear deletes all key and value present in the Gache.
func Clear() {
	instance.Clear()
}
