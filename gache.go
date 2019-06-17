package gache

import (
	"context"
	"encoding/gob"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/kpango/fastime"

	// "github.com/pierrec/lz4"
	"golang.org/x/sync/singleflight"
)

type (
	// Gache is base interface type
	Gache interface {
		Clear()
		Delete(string)
		DeleteExpired(context.Context) uint64
		DisableExpiredHook() Gache
		EnableExpiredHook() Gache
		Foreach(context.Context, func(string, interface{}, int64) bool) Gache
		Get(string) (interface{}, bool)
		GetWithExpire(string) (interface{}, int64, bool)
		Read(io.Reader) error
		Set(string, interface{})
		SetDefaultExpire(time.Duration) Gache
		SetExpiredHook(f func(context.Context, string)) Gache
		SetWithExpire(string, interface{}, time.Duration)
		StartExpired(context.Context, time.Duration) Gache
		Len() int
		ToMap(context.Context) *sync.Map
		ToRawMap(context.Context) map[string]interface{}
		Write(context.Context, io.Writer) error
		Stop()

		// TODO Future works below
		// func ExtendExpire(string, addExp time.Duration){}
		// func (g *gache)ExtendExpire(string, addExp time.Duration){}
		// func GetRefresh(string)(interface{}, bool){}
		// func (g *gache)GetRefresh(string)(interface{}, bool){}
		// func GetRefreshWithDur(string, time.Duration)(interface{}, bool){}
		// func (g *gache)GetRefreshWithDur(string, time.Duration)(interface{}, bool){}
		// func GetWithIgnoredExpire(string)(interface{}, bool){}
		// func (g *gache)GetWithIgnoredExpire(string)(interface{}, bool){}
		// func Keys(context.Context)[]string{}
		// func (g *gache)Keys(context.Context)[]string{}
		// func Pop(string)(interface{}, bool) // Get & Delete{}
		// func (g *gache)Pop(string)(interface{}, bool) // Get & Delete{}
		// func SetIfNotExists(string, interface{}){}
		// func (g *gache)SetIfNotExists(string, interface{}){}
		// func SetWithExpireIfNotExists(string, interface{}, time.Duration){}
		// func (g *gache)SetWithExpireIfNotExists(string, interface{}, time.Duration){}
	}

	// gache is base instance type
	gache struct {
		expChan        chan string
		expFunc        func(context.Context, string)
		expFuncEnabled bool
		expGroup       singleflight.Group
		cancel         context.CancelFunc
		expire         int64
		l              uint64
		shards         [slen]*Map
	}

	value struct {
		expire int64
		val    interface{}
	}
)

const (
	// slen is shards length
	slen = 512
	// mask is slen-1 Hex value
	mask = 0x1FF
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
		expire: int64(time.Second * 30),
	}
	for i := range g.shards {
		g.shards[i] = new(Map)
	}
	g.expChan = make(chan string, len(g.shards)*10)
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
	return v.expire <= 0 || fastime.UnixNanoNow() <= v.expire
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
		ctx, g.cancel = context.WithCancel(ctx)
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
	m := make(map[string]interface{}, g.Len())
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
func (g *gache) get(key string) (interface{}, int64, bool) {
	v, ok := g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))&mask].Load(key)

	if !ok {
		return nil, 0, false
	}

	if v.isValid() {
		return v.val, v.expire, true
	}

	g.expiration(key)
	return nil, 0, false
}

// Get returns value & exists from key
func (g *gache) Get(key string) (interface{}, bool) {
	v, _, ok := g.get(key)
	return v, ok
}

// Get returns value & exists from key
func Get(key string) (interface{}, bool) {
	v, _, ok := instance.get(key)
	return v, ok
}

// GetWithExpire returns value & expire & exists from key
func (g *gache) GetWithExpire(key string) (interface{}, int64, bool) {
	return g.get(key)
}

// GetWithExpire returns value & expire & exists from key
func GetWithExpire(key string) (interface{}, int64, bool) {
	return instance.get(key)
}

// set sets key-value & expiration to Gache
func (g *gache) set(key string, val interface{}, expire int64) {
	if expire > 0 {
		expire = fastime.UnixNanoNow() + expire
	}
	atomic.AddUint64(&g.l, 1)
	g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))&mask].Store(key, value{
		expire: expire,
		val:    val,
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
	atomic.AddUint64(&g.l, ^uint64(0))
	g.shards[xxhash.Sum64(*(*[]byte)(unsafe.Pointer(&key)))&mask].Delete(key)
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
			g.shards[idx].Range(func(k string, v value) bool {
				select {
				case <-c.Done():
					return false
				default:
					if !v.isValid() {
						g.expiration(k)
						atomic.AddUint64(&rows, 1)
					}
					return true
				}
			})
			wg.Done()
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
			g.shards[idx].Range(func(k string, v value) bool {
				select {
				case <-c.Done():
					return false
				default:
					if v.isValid() {
						return f(k, v.val, v.expire)
					}
					g.expiration(k)
					return true
				}
			})
			wg.Done()
		}(ctx, i)
	}
	wg.Wait()
	return g
}

// Foreach calls f sequentially for each key and value present in the Gache.
func Foreach(ctx context.Context, f func(string, interface{}, int64) bool) Gache {
	return instance.Foreach(ctx, f)
}

// Len returns stored object length
func Len() int {
	return instance.Len()
}

// Len returns stored object length
func (g *gache) Len() int {
	l := atomic.LoadUint64(&g.l)
	return *(*int)(unsafe.Pointer(&l))
}

// Write writes all cached data to writer
func (g *gache) Write(ctx context.Context, w io.Writer) error {
	mu := new(sync.Mutex)
	m := make(map[string]interface{}, g.Len())

	g.Foreach(ctx, func(key string, val interface{}, exp int64) bool {
		gob.Register(val)
		mu.Lock()
		m[key] = val
		mu.Unlock()
		return true
	})
	gob.Register(map[string]interface{}{})

	return gob.NewEncoder(w).Encode(&m)
	// return gob.NewEncoder(lz4.NewWriter(w)).Encode(&m)
}

// Write writes all cached data to writer
func Write(ctx context.Context, w io.Writer) error {
	return instance.Write(ctx, w)
}

// Read reads reader data to cache
func (g *gache) Read(r io.Reader) error {
	var m map[string]interface{}
	gob.Register(map[string]interface{}{})
	err := gob.NewDecoder(r).Decode(&m)
	// err := gob.NewDecoder(lz4.NewReader(r)).Decode(&m)
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

func (g *gache) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
}

func Stop() {
	instance.Stop()
}

// Clear deletes all key and value present in the Gache.
func (g *gache) Clear() {
	for i := range g.shards {
		g.shards[i] = new(Map)
	}
}

// Clear deletes all key and value present in the Gache.
func Clear() {
	instance.Clear()
}
