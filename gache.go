package gache

import (
	"context"
	"encoding/gob"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/kpango/fastime"
	"github.com/pierrec/lz4"
	"golang.org/x/sync/singleflight"
)

type (
	// Gache is base interface type
	Gache interface {
		Clear()
		Delete(string)
		DeleteExpired(context.Context) uint64
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
		ToRawMap(context.Context) map[string]interface{}
		Read(io.Reader) error
		Write(context.Context, io.Writer) error
	}

	// gache is base instance type
	gache struct {
		l              uint64
		shards         [255]*sync.Map
		expire         int64
		expFuncEnabled bool
		expFunc        func(context.Context, string)
		expChan        chan string
		expGroup       singleflight.Group
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
		expire:  int64(time.Second * 30),
		expChan: make(chan string, 1000),
	}
	g.l = uint64(len(g.shards))
	for i := range g.shards {
		g.shards[i] = new(sync.Map)
	}
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
	return v.expire == 0 || fastime.UnixNanoNow() < v.expire
}

// SetDefaultExpire set expire duration
func (g *gache) SetDefaultExpire(ex time.Duration) Gache {
	atomic.StoreInt64(&g.expire, *(*int64)(unsafe.Pointer(&ex)))
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

// ToMap returns All Cache Key-Value sync.Map
func (g *gache) ToMap(ctx context.Context) *sync.Map {
	m := new(sync.Map)
	g.Foreach(ctx, func(key string, val interface{}, exp int64) bool {
		go m.Store(key, val)
		return true
	})

	return m
}

// ToMap returns All Cache Key-Value sync.Map
func ToMap(ctx context.Context) *sync.Map {
	return instance.ToMap(ctx)
}

// ToRawMap returns All Cache Key-Value map
func (g *gache) ToRawMap(ctx context.Context) map[string]interface{} {
	m := make(map[string]interface{})
	mu := new(sync.Mutex)
	g.Foreach(ctx, func(key string, val interface{}, exp int64) bool {
		mu.Lock()
		m[key] = val
		mu.Unlock()
		return true
	})
	return m
}

// ToRawMap returns All Cache Key-Value map
func ToRawMap(ctx context.Context) map[string]interface{} {
	return instance.ToRawMap(ctx)
}

// get returns value & exists from key
func (g *gache) get(key string) (interface{}, bool) {
	v, ok := g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))%g.l].Load(key)

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
func (g *gache) set(key string, val interface{}, expire int64) {
	g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))%g.l].Store(key, &value{
		expire: fastime.UnixNanoNow() + expire,
		val:    &val,
	})
}

// SetWithExpire sets key-value & expiration to Gache
func (g *gache) SetWithExpire(key string, val interface{}, expire time.Duration) {
	g.set(key, val, *(*int64)(unsafe.Pointer(&expire)))
}

// SetWithExpire sets key-value & expiration to Gache
func SetWithExpire(key string, val interface{}, expire time.Duration) {
	instance.set(key, val, *(*int64)(unsafe.Pointer(&expire)))
}

// Set sets key-value to Gache using default expiration
func (g *gache) Set(key string, val interface{}) {
	g.set(key, val, atomic.LoadInt64(&g.expire))
}

// Set sets key-value to Gache using default expiration
func Set(key string, val interface{}) {
	instance.set(key, val, atomic.LoadInt64(&instance.expire))
}

// Delete deletes value from Gache using key
func (g *gache) Delete(key string) {
	g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))%g.l].Delete(key)
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
func (g *gache) DeleteExpired(ctx context.Context) uint64 {
	wg := new(sync.WaitGroup)
	var rows uint64
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			g.shards[idx].Range(func(k, v interface{}) bool {
				select {
				case <-c.Done():
					return false
				default:
					d, ok := v.(*value)
					if ok {
						if !d.isValid() {
							g.expiration(k.(string))
							atomic.AddUint64(&rows, 1)
						}
						return true
					}
					return false
				}
			})
		}(ctx, i)
	}
	wg.Wait()
	return atomic.LoadUint64(&rows)
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func DeleteExpired(ctx context.Context) uint64 {
	return instance.DeleteExpired(ctx)
}

// Foreach calls f sequentially for each key and value present in the Gache.
func (g *gache) Foreach(ctx context.Context, f func(string, interface{}, int64) bool) Gache {
	wg := new(sync.WaitGroup)
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			g.shards[idx].Range(func(k, v interface{}) bool {
				select {
				case <-c.Done():
					return false
				default:
					d, ok := v.(*value)
					if ok {
						if !d.isValid() {
							g.expiration(k.(string))
							return true
						}
						return f(k.(string), *d.val, d.expire)
					}
					return false
				}
			})
		}(ctx, i)
	}
	wg.Wait()
	return g
}

// Foreach calls f sequentially for each key and value present in the Gache.
func Foreach(ctx context.Context, f func(string, interface{}, int64) bool) Gache {
	return instance.Foreach(ctx, f)
}

// Write writes all cached data to writer
func (g *gache) Write(ctx context.Context, w io.Writer) error {
	return gob.NewEncoder(lz4.NewWriter(w)).Encode(g.ToRawMap(ctx))
}

// Write writes all cached data to writer
func Write(ctx context.Context, w io.Writer) error {
	return instance.Write(ctx, w)
}

// Read reads reader data to cache
func (g *gache) Read(r io.Reader) error {
	m := make(map[string]interface{})
	err := gob.NewDecoder(lz4.NewReader(r)).Decode(&m)
	if err != nil {
		return err
	}
	for k, v := range m {
		g.Set(k, v)
	}
	return nil
}

// Read reads reader data to cache
func Read(r io.Reader) error {
	return instance.Read(r)
}

// Clear deletes all key and value present in the Gache.
func (g *gache) Clear() {
	for i := range g.shards {
		g.shards[i] = new(sync.Map)
	}
}

// Clear deletes all key and value present in the Gache.
func Clear() {
	instance.Clear()
}
