// Package gache provides an ultra-fast caching experience for users.
package gache

import (
	"context"
	"encoding/gob"
	"hash/maphash"
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
		Delete(string) (V, bool)
		DeleteExpired(context.Context) uint64
		DisableExpiredHook() Gache[V]
		EnableExpiredHook() Gache[V]
		Range(context.Context, func(string, V, int64) bool) Gache[V]
		Get(string) (V, bool)
		GetWithExpire(string) (V, int64, bool)
		Read(io.Reader) error
		Set(string, V)
		SetDefaultExpire(time.Duration) Gache[V]
		SetExpiredHook(f func(context.Context, string, V)) Gache[V]
		SetWithExpire(string, V, time.Duration)
		StartExpired(context.Context, time.Duration) Gache[V]
		Len() int
		Size() uintptr
		ToMap(context.Context) *sync.Map
		ToRawMap(context.Context) map[string]V
		Write(context.Context, io.Writer) error
		Stop()

		ExtendExpire(string, time.Duration)
		GetRefresh(string) (V, bool)
		GetRefreshWithDur(string, time.Duration) (V, bool)
		GetWithIgnoredExpire(string) (V, bool)
		Keys(context.Context) []string
		Values(context.Context) []V
		Pop(string) (V, bool)
		SetIfNotExists(string, V)
		SetWithExpireIfNotExists(string, V, time.Duration)
	}

	// gache is base instance type
	gache[V any] struct {
		shards         [slen]*Map[string, value[V]]
		cancel         atomic.Pointer[context.CancelFunc]
		expChan        chan kv[V]
		expFunc        func(context.Context, string, V)
		valPool        *sync.Pool
		expFuncEnabled bool
		expire         int64
		maxKeyLength   uint64
	}

	value[V any] struct {
		val    V
		expire int64
	}

	kv[V any] struct {
		key   string
		value V
	}
)

const (
	// slen is shards length
	slen = 4096
	// slen = 512
	// mask is slen-1 Hex value
	mask = 0xFFF
	// mask = 0x1FF

	// NoTTL can be use for disabling ttl cache expiration
	NoTTL time.Duration = -1
)

// hashSeed is initialized once at package load time and shared by all cache instances.
// This is an intentional design choice to avoid per-instance seed management overhead.
// If your threat model requires each cache instance to have a distinct hash seed for
// stronger isolation against collision attacks, do not assume per-instance seeding.
var hashSeed = maphash.MakeSeed()

// New returns Gache (*gache) instance
func New[V any](opts ...Option[V]) Gache[V] {
	g := new(gache[V])
	g.valPool = &sync.Pool{
		New: func() any {
			return new(value[V])
		},
	}
	for i := range g.shards {
		g.shards[i] = newMap[V]()
	}
	for _, opt := range append([]Option[V]{
		WithDefaultExpiration[V](30 * time.Second),
		WithMaxKeyLength[V](256),
	}, opts...) {
		opt(g)
	}
	g.expChan = make(chan kv[V], len(g.shards)*10)
	return g
}

func newMap[V any]() (m *Map[string, value[V]]) {
	return new(Map[string, value[V]])
}

func getShardID(key string, kl uint64) (id uint64) {
	lk := uint64(len(key))
	if lk == 0 {
		return 0
	}
	if kl != 0 && lk > kl {
		key = key[:kl]
		lk = kl
	}
	if lk == 1 {
		return uint64(key[0]) & mask
	}
	if lk <= 32 {
		return maphash.String(hashSeed, key) & mask
	}
	return xxh3.HashString(key) & mask
}

// isValid checks expiration of value
func (v *value[V]) isValid() (valid bool) {
	return v.expire <= 0 || fastime.UnixNanoNow() <= v.expire
}

// reset zeros out all fields to prevent memory leaks from retained references
// when the value object is returned to the pool for reuse.
func (v *value[V]) reset() {
	var zero V
	v.val = zero
	v.expire = 0
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
func (g *gache[V]) SetExpiredHook(f func(context.Context, string, V)) Gache[V] {
	g.expFunc = f
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
			case ex := <-g.expChan:
				go g.expFunc(ctx, ex.key, ex.value)
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
func (g *gache[V]) ToMap(ctx context.Context) (m *sync.Map) {
	m = new(sync.Map)
	_ = g.loop(ctx, func(k string, v *value[V]) bool {
		m.Store(k, v.val)
		return true
	})
	return m
}

// ToRawMap returns All Cache Key-Value map
func (g *gache[V]) ToRawMap(ctx context.Context) (m map[string]V) {
	m = make(map[string]V, g.Len())
	for i := range g.shards {
		select {
		case <-ctx.Done():
			return m
		default:
			g.shards[i].RangePointer(func(k string, v *value[V]) bool {
				if v != nil {
					if !v.isValid() {
						g.expiration(k)
					} else {
						m[k] = v.val
					}
				}
				return true
			})
		}
	}
	return m
}

// Keys returns all keys in the Gache.
func (g *gache[V]) Keys(ctx context.Context) (keys []string) {
	keys = make([]string, 0, g.Len())
	for i := range g.shards {
		select {
		case <-ctx.Done():
			return keys
		default:
			g.shards[i].RangePointer(func(k string, v *value[V]) bool {
				if v != nil {
					if !v.isValid() {
						g.expiration(k)
					} else {
						keys = append(keys, k)
					}
				}
				return true
			})
		}
	}
	return keys
}

// Values returns all values in the Gache.
func (g *gache[V]) Values(ctx context.Context) (values []V) {
	values = make([]V, 0, g.Len())
	for i := range g.shards {
		select {
		case <-ctx.Done():
			return values
		default:
			g.shards[i].RangePointer(func(k string, v *value[V]) bool {
				if v != nil {
					if !v.isValid() {
						g.expiration(k)
					} else {
						values = append(values, v.val)
					}
				}
				return true
			})
		}
	}
	return values
}

// get returns value & exists from key
func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	var val *value[V]
	val, ok = g.shards[getShardID(key, g.maxKeyLength)].LoadPointer(key)
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
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	newVal := g.valPool.Get().(*value[V])
	newVal.val = val
	newVal.expire = expire
	old, loaded := shard.SwapPointer(key, newVal)
	if loaded {
		old.reset()
		g.valPool.Put(old)
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
func (g *gache[V]) Delete(key string) (v V, loaded bool) {
	var val *value[V]
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded = shard.LoadAndDeletePointer(key)
	if loaded {
		v = val.val
		val.reset()
		g.valPool.Put(val)
		return v, loaded
	}
	return v, false
}

func (g *gache[V]) expiration(key string) {
	v, loaded := g.Delete(key)
	if loaded && g.expFuncEnabled {
		g.expChan <- kv[V]{key: key, value: v}
	}
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func (g *gache[V]) DeleteExpired(ctx context.Context) (expired uint64) {
	return g.loop(ctx, func(k string, v *value[V]) bool {
		return true
	})
}

// Range calls f sequentially for each key and value present in the Gache.
func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	_ = g.loop(ctx, func(k string, v *value[V]) bool {
		return f(k, v.val, v.expire)
	})
	return g
}

func (g *gache[V]) loop(ctx context.Context, f func(string, *value[V]) bool) (expiredRows uint64) {
	var (
		idx atomic.Uint64
		wg  sync.WaitGroup
	)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			var expires uint64
			for {
				i := idx.Add(1) - 1
				if i >= slen {
					atomic.AddUint64(&expiredRows, expires)
					return
				}
				select {
				case <-ctx.Done():
					atomic.AddUint64(&expiredRows, expires)
					return
				default:
					g.shards[i].RangePointer(func(k string, v *value[V]) (ok bool) {
						if v != nil {
							switch {
							case !v.isValid():
								g.expiration(k)
								expires++
							case f != nil:
								return f(k, v)
							default:
							}
						}
						return true
					})
				}
			}
		})
	}
	wg.Wait()
	return atomic.LoadUint64(&expiredRows)
}

// Len returns stored object length
func (g *gache[V]) Len() (l int) {
	for i := range g.shards {
		l += g.shards[i].Len()
	}
	return l
}

func (g *gache[V]) Size() (size uintptr) {
	size += unsafe.Sizeof(g.expFuncEnabled) // bool
	size += unsafe.Sizeof(g.expire)         // int64
	size += unsafe.Sizeof(g.cancel)         // atomic.Pointer[context.CancelFunc]
	size += unsafe.Sizeof(g.expChan)        // chan kv[V]
	size += unsafe.Sizeof(g.expFunc)        // func(context.Context, string, V)
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
		if g.shards[i] == nil {
			g.shards[i] = newMap[V]()
		} else {
			g.shards[i].Clear()
		}
	}
}

// ExtendExpire extends the expiration of the key by addExp duration.
func (g *gache[V]) ExtendExpire(key string, addExp time.Duration) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	var newVal *value[V]
	for {
		val, ok := shard.LoadPointer(key)
		if !ok {
			if newVal != nil {
				newVal.reset()
				g.valPool.Put(newVal)
			}
			return
		}
		if !val.isValid() {
			g.expiration(key)
			if newVal != nil {
				newVal.reset()
				g.valPool.Put(newVal)
			}
			return
		}

		if newVal == nil {
			newVal = g.valPool.Get().(*value[V])
		}
		newVal.val = val.val
		newVal.expire = val.expire + int64(addExp)

		if shard.CompareAndSwapPointer(key, val, newVal) {
			val.reset()
			g.valPool.Put(val)
			return
		}
	}
}

// GetRefresh returns value & exists from key and refreshes the expiration.
func (g *gache[V]) GetRefresh(key string) (V, bool) {
	return g.GetRefreshWithDur(key, time.Duration(atomic.LoadInt64(&g.expire)))
}

// GetRefreshWithDur returns value & exists from key and refreshes the expiration with d duration.
func (g *gache[V]) GetRefreshWithDur(key string, d time.Duration) (v V, ok bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	var newVal *value[V]
	for {
		val, ok := shard.LoadPointer(key)
		if !ok {
			if newVal != nil {
				newVal.reset()
				g.valPool.Put(newVal)
			}
			return v, false
		}
		if !val.isValid() {
			g.expiration(key)
			if newVal != nil {
				newVal.reset()
				g.valPool.Put(newVal)
			}
			return v, false
		}

		if newVal == nil {
			newVal = g.valPool.Get().(*value[V])
		}
		newVal.val = val.val
		newVal.expire = fastime.UnixNanoNow() + int64(d)

		if shard.CompareAndSwapPointer(key, val, newVal) {
			val.reset()
			g.valPool.Put(val)
			return newVal.val, true
		}
	}
}

// GetWithIgnoredExpire returns value & exists from key, ignoring expiration.
func (g *gache[V]) GetWithIgnoredExpire(key string) (v V, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].LoadPointer(key)
	if !ok {
		return v, false
	}
	return val.val, true
}

// Pop returns value & exists from key and deletes it.
func (g *gache[V]) Pop(key string) (v V, ok bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded := shard.LoadAndDeletePointer(key)
	if !loaded {
		return v, false
	}
	v = val.val
	valid := val.isValid()
	val.reset()
	g.valPool.Put(val)
	if valid {
		return v, true
	}
	if g.expFuncEnabled {
		g.expChan <- kv[V]{key: key, value: v}
	}
	return v, false
}

// SetIfNotExists sets key-value to Gache if it does not exist.
func (g *gache[V]) SetIfNotExists(key string, val V) {
	g.SetWithExpireIfNotExists(key, val, time.Duration(atomic.LoadInt64(&g.expire)))
}

// SetWithExpireIfNotExists sets key-value & expiration to Gache if it does not exist.
func (g *gache[V]) SetWithExpireIfNotExists(key string, val V, d time.Duration) {
	exp := int64(d)
	if exp > 0 {
		exp += fastime.UnixNanoNow()
	}

	newVal := g.valPool.Get().(*value[V])
	newVal.val = val
	newVal.expire = exp

	shard := g.shards[getShardID(key, g.maxKeyLength)]
	for {
		actual, loaded := shard.LoadOrStorePointer(key, newVal)
		if !loaded {
			return
		}

		// loaded: actual is the existing value (*value[V])

		if actual.isValid() {
			// New value not used
			newVal.reset()
			g.valPool.Put(newVal)
			return
		}

		// actual is expired. Replace it.
		if shard.CompareAndSwapPointer(key, actual, newVal) {
			// We replaced actual with newVal.
			actual.reset()
			g.valPool.Put(actual)
			return
		}
		// CAS failed, loop again.
	}
}
