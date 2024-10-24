package gache

import (
	"context"
	"encoding/gob"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/kpango/fastime"
	"github.com/zeebo/xxh3"
)

type (
	// Gache is base interface type
	Gache[V any] interface {
		Clear()
		Delete(string) (bool, V)
		DeleteExpired(context.Context) uint64
		DisableExpiredHook() Gache[V]
		EnableExpiredHook() Gache[V]
		Range(context.Context, func(string, V, int64) bool) Gache[V]
		Get(string) (V, bool)
		GetWithExpire(string) (V, int64, bool)
		Read(io.Reader) error
		Set(string, V)
		SetDefaultExpire(time.Duration) Gache[V]
		SetExpiredHook(f func(context.Context, string)) Gache[V]
		SetExpiredHookWithValue(f func(context.Context, string, V)) Gache[V]
		SetWithExpire(string, V, time.Duration)
		StartExpired(context.Context, time.Duration) Gache[V]
		Len() int
		Size() uintptr
		ToMap(context.Context) *sync.Map
		ToRawMap(context.Context) map[string]V
		Write(context.Context, io.Writer) error
		Stop()

		// TODO Future works below
		// func ExtendExpire(string, addExp time.Duration){}
		// func (g *gache)ExtendExpire(string, addExp time.Duration){}
		// func GetRefresh(string)(V, bool){}
		// func (g *gache)GetRefresh(string)(V, bool){}
		// func GetRefreshWithDur(string, time.Duration)(V, bool){}
		// func (g *gache)GetRefreshWithDur(string, time.Duration)(V, bool){}
		// func GetWithIgnoredExpire(string)(V, bool){}
		// func (g *gache)GetWithIgnoredExpire(string)(V, bool){}
		// func Keys(context.Context)[]string{}
		// func (g *gache)Keys(context.Context)[]string{}
		// func Pop(string)(V, bool) // Get & Delete{}
		// func (g *gache)Pop(string)(V, bool) // Get & Delete{}
		// func SetIfNotExists(string, V){}
		// func (g *gache)SetIfNotExists(string, V){}
		// func SetWithExpireIfNotExists(string, V, time.Duration){}
		// func (g *gache)SetWithExpireIfNotExists(string, V, time.Duration){}
	}

	// gache is base instance type
	gache[V any] struct {
		shards           [slen]*Map[string, *value[V]]
		cancel           atomic.Pointer[context.CancelFunc]
		expChan          chan keyValue[V]
		expFunc          func(context.Context, string)
		expFuncWithValue func(context.Context, string, V)
		expFuncEnabled   bool
		expire           int64
		l                uint64
	}

	value[V any] struct {
		val    V
		expire int64
	}

	keyValue[V any] struct {
		key   string
		value V
	}
)

const (
	// slen is shards length
	slen = 512
	// slen = 4096
	// mask is slen-1 Hex value
	mask = 0x1FF
	// mask = 0xFFF

	// NoTTL can be use for disabling ttl cache expiration
	NoTTL time.Duration = -1

	maxHashKeyLength = 256
)

// New returns Gache (*gache) instance
func New[V any](opts ...Option[V]) Gache[V] {
	g := new(gache[V])
	for _, opt := range append([]Option[V]{
		WithDefaultExpiration[V](time.Second * 30),
	}, opts...) {
		opt(g)
	}
	g.Clear()
	g.expChan = make(chan keyValue[V], len(g.shards)*10)
	return g
}

func newMap[V any]() (m *Map[string, *value[V]]) {
	return new(Map[string, *value[V]])
}

func getShardID(key string) (id uint64) {
	if len(key) > maxHashKeyLength {
		return xxh3.HashString(key[:maxHashKeyLength]) & mask
	}
	return xxh3.HashString(key) & mask
}

// isValid checks expiration of value
func (v *value[V]) isValid() (valid bool) {
	return v.expire <= 0 || fastime.UnixNanoNow() <= v.expire
}

// SetDefaultExpire set expire duration
func (g *gache[V]) SetDefaultExpire(ex time.Duration) Gache[V] {
	atomic.StoreInt64(&g.expire, *(*int64)(unsafe.Pointer(&ex)))
	return g
}

// EnableExpiredHook enables expired hook function
func (g *gache[V]) EnableExpiredHook() Gache[V] {
	g.expFuncEnabled = true
	return g
}

// DisableExpiredHook disables expired hook function
func (g *gache[V]) DisableExpiredHook() Gache[V] {
	g.expFuncEnabled = false
	return g
}

// SetExpiredHook set expire hooked function
func (g *gache[V]) SetExpiredHook(f func(context.Context, string)) Gache[V] {
	g.expFunc = f
	return g
}

// SetExpiredHookWithValue set expire hooked function
func (g *gache[V]) SetExpiredHookWithValue(f func(context.Context, string, V)) Gache[V] {
	g.expFuncWithValue = f
	return g
}

// StartExpired starts delete expired value daemon
func (g *gache[V]) StartExpired(ctx context.Context, dur time.Duration) Gache[V] {
	go func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		g.cancel.Store(&cancel)
		tick := time.NewTicker(dur)
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				return
			case keyValue := <-g.expChan:
				if g.expFunc != nil {
					go g.expFunc(ctx, keyValue.key)
				}
				if g.expFuncWithValue != nil {
					go g.expFuncWithValue(ctx, keyValue.key, keyValue.value)
				}
			case <-tick.C:
				go func() {
					g.DeleteExpired(ctx)
					runtime.Gosched()
				}()
			}
		}
	}()
	return g
}

// ToMap returns All Cache Key-Value sync.Map
func (g *gache[V]) ToMap(ctx context.Context) *sync.Map {
	m := new(sync.Map)
	var wg sync.WaitGroup
	defer wg.Wait()
	g.Range(ctx, func(key string, val V, exp int64) (ok bool) {
		wg.Add(1)
		go func() {
			m.Store(key, val)
			wg.Done()
		}()
		return true
	})

	return m
}

// ToRawMap returns All Cache Key-Value map
func (g *gache[V]) ToRawMap(ctx context.Context) map[string]V {
	m := make(map[string]V, g.Len())
	mu := new(sync.Mutex)
	g.Range(ctx, func(key string, val V, exp int64) (ok bool) {
		mu.Lock()
		m[key] = val
		mu.Unlock()
		return true
	})
	return m
}

// get returns value & exists from key
func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	var val *value[V]
	shard := g.shards[getShardID(key)]
	val, ok = shard.Load(key)
	if !ok {
		return v, 0, false
	}

	if val.isValid() {
		return val.val, val.expire, true
	}

	g.expiration(key)
	return v, val.expire, false
}

// Get returns value & exists from key
func (g *gache[V]) Get(key string) (v V, ok bool) {
	v, _, ok = g.get(key)
	return v, ok
}

// GetWithExpire returns value & expire & exists from key
func (g *gache[V]) GetWithExpire(key string) (v V, expire int64, ok bool) {
	return g.get(key)
}

// set sets key-value & expiration to Gache
func (g *gache[V]) set(key string, val V, expire int64) {
	if expire > 0 {
		expire = fastime.UnixNanoNow() + expire
	}
	shard := g.shards[getShardID(key)]
	_, loaded := shard.Swap(key, &value[V]{
		expire: expire,
		val:    val,
	})
	if !loaded {
		atomic.AddUint64(&g.l, 1)
	}
}

// SetWithExpire sets key-value & expiration to Gache
func (g *gache[V]) SetWithExpire(key string, val V, expire time.Duration) {
	g.set(key, val, *(*int64)(unsafe.Pointer(&expire)))
}

// Set sets key-value to Gache using default expiration
func (g *gache[V]) Set(key string, val V) {
	g.set(key, val, atomic.LoadInt64(&g.expire))
}

// Delete deletes value from Gache using key
func (g *gache[V]) Delete(key string) (loaded bool, v V) {
	var val *value[V]
	val, loaded = g.shards[getShardID(key)].LoadAndDelete(key)
	if loaded {
		atomic.AddUint64(&g.l, ^uint64(0))
	}
	return loaded, val.val
}

func (g *gache[V]) expiration(key string) {
	_, v := g.Delete(key)

	if g.expFuncEnabled {
		g.expChan <- keyValue[V]{key: key, value: v}
	}
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func (g *gache[V]) DeleteExpired(ctx context.Context) (rows uint64) {
	var wg sync.WaitGroup
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			select {
			case <-c.Done():
				return
			default:
				g.shards[idx].Range(func(k string, v *value[V]) (ok bool) {
					if !v.isValid() {
						g.expiration(k)
						atomic.AddUint64(&rows, 1)
					}
					return true
				})
			}
		}(ctx, i)
	}
	wg.Wait()
	return atomic.LoadUint64(&rows)
}

// Range calls f sequentially for each key and value present in the Gache.
func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	wg := new(sync.WaitGroup)
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			select {
			case <-c.Done():
				return
			default:
				g.shards[idx].Range(func(k string, v *value[V]) (ok bool) {
					if v.isValid() {
						return f(k, v.val, v.expire)
					}
					g.expiration(k)
					return true
				})
			}
		}(ctx, i)
	}
	wg.Wait()
	return g
}

// Len returns stored object length
func (g *gache[V]) Len() int {
	l := atomic.LoadUint64(&g.l)
	return *(*int)(unsafe.Pointer(&l))
}

func (g *gache[V]) Size() (size uintptr) {
	size += unsafe.Sizeof(g.expFuncEnabled)   // bool
	size += unsafe.Sizeof(g.expire)           // int64
	size += unsafe.Sizeof(g.l)                // uint64
	size += unsafe.Sizeof(g.cancel)           // atomic.Pointer[context.CancelFunc]
	size += unsafe.Sizeof(g.expChan)          // chan keyValue[V]
	size += unsafe.Sizeof(g.expFunc)          // func(context.Context, string)
	size += unsafe.Sizeof(g.expFuncWithValue) // func(context.Context, string, V)
	for _, shard := range g.shards {
		size += shard.Size()
	}
	return size
}

// Write writes all cached data to writer
func (g *gache[V]) Write(ctx context.Context, w io.Writer) error {
	m := g.ToRawMap(ctx)
	gob.Register(map[string]V{})
	return gob.NewEncoder(w).Encode(&m)
}

// Read reads reader data to cache
func (g *gache[V]) Read(r io.Reader) error {
	var m map[string]V
	gob.Register(map[string]V{})
	err := gob.NewDecoder(r).Decode(&m)
	if err != nil {
		return err
	}
	for k, v := range m {
		go g.Set(k, v)
	}
	return nil
}

// Stop kills expire daemon
func (g *gache[V]) Stop() {
	if c := g.cancel.Load(); c != nil {
		cancel := *c
		cancel()
	}
}

// Clear deletes all key and value present in the Gache.
func (g *gache[V]) Clear() {
	for i := range g.shards {
		g.shards[i] = newMap[V]()
	}
}

func (v *value[V]) Size() uintptr {
	var size uintptr

	size += unsafe.Sizeof(v.expire) // int64
	size += unsafe.Sizeof(v.val)    // V size

	return size
}
