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
		Pop(string) (V, bool)
		SetIfNotExists(string, V)
		SetWithExpireIfNotExists(string, V, time.Duration)
	}

	// gache is base instance type
	gache[V any] struct {
		shards         [slen]*Map[string, *value[V]]
		cancel         atomic.Pointer[context.CancelFunc]
		expChan        chan keyValue[V]
		expFunc        func(context.Context, string, V)
		expFuncEnabled bool
		expire         int64
		l              uint64
		maxKeyLength   uint64
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
)

// hashSeed is initialized once at package load time and shared by all cache instances.
// This is an intentional design choice to avoid per-instance seed management overhead.
// If your threat model requires each cache instance to have a distinct hash seed for
// stronger isolation against collision attacks, do not assume per-instance seeding.
var hashSeed = maphash.MakeSeed()

// New returns Gache (*gache) instance
func New[V any](opts ...Option[V]) Gache[V] {
	g := new(gache[V])
	for _, opt := range append([]Option[V]{
		WithDefaultExpiration[V](30 * time.Second),
		WithMaxKeyLength[V](256),
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

func getShardID(key string, kl uint64) (id uint64) {
	if kl != 0 {
		kl = min(uint64(len(key)), kl)
		if kl == 1 {
			return uint64(key[0]) & mask
		}
		if kl <= 32 {
			return maphash.String(hashSeed, key[:kl]) & mask
		}
		return xxh3.HashString(key[:kl]) & mask
	}
	if len(key) == 1 {
		return uint64(key[0]) & mask
	}
	if len(key) <= 32 {
		return maphash.String(hashSeed, key) & mask
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
			case kv := <-g.expChan:
				go g.expFunc(ctx, kv.key, kv.value)
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
		wg.Go(func() {
			m.Store(key, val)
		})
		return true
	})
	return m
}

// ToRawMap returns All Cache Key-Value map
func (g *gache[V]) ToRawMap(ctx context.Context) map[string]V {
	m := make(map[string]V, g.Len())
	for i := range g.shards {
		select {
		case <-ctx.Done():
			return m
		default:
			g.shards[i].Range(func(k string, v *value[V]) bool {
				if v.isValid() {
					m[k] = v.val
				} else {
					g.expiration(k)
				}
				return true
			})
		}
	}
	return m
}

// get returns value & exists from key
func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	var val *value[V]
	shard := g.shards[getShardID(key, g.maxKeyLength)]
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
	shard := g.shards[getShardID(key, g.maxKeyLength)]
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
func (g *gache[V]) Delete(key string) (v V, loaded bool) {
	var val *value[V]
	val, loaded = g.shards[getShardID(key, g.maxKeyLength)].LoadAndDelete(key)
	if loaded {
		atomic.AddUint64(&g.l, ^uint64(0))
	}
	if val != nil && loaded {
		return val.val, loaded
	}
	return v, loaded
}

func (g *gache[V]) expiration(key string) {
	v, loaded := g.Delete(key)

	if loaded && g.expFuncEnabled {
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
	size += unsafe.Sizeof(g.expFuncEnabled) // bool
	size += unsafe.Sizeof(g.expire)         // int64
	size += unsafe.Sizeof(g.l)              // uint64
	size += unsafe.Sizeof(g.cancel)         // atomic.Pointer[context.CancelFunc]
	size += unsafe.Sizeof(g.expChan)        // chan keyValue[V]
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

func (v *value[V]) Size() (size uintptr) {
	return unsafe.Sizeof(v.expire) + unsafe.Sizeof(v.val)
}

// ExtendExpire extends the expiration of the key by addExp duration.
func (g *gache[V]) ExtendExpire(key string, addExp time.Duration) {
	for {
		shard := g.shards[getShardID(key, g.maxKeyLength)]
		val, ok := shard.Load(key)
		if !ok {
			return
		}
		if !val.isValid() {
			g.expiration(key)
			return
		}

		newVal := &value[V]{
			val:    val.val,
			expire: val.expire + int64(addExp),
		}
		if shard.CompareAndSwap(key, val, newVal) {
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
	for {
		shard := g.shards[getShardID(key, g.maxKeyLength)]
		val, ok := shard.Load(key)
		if !ok {
			return v, false
		}
		if !val.isValid() {
			g.expiration(key)
			return v, false
		}

		newVal := &value[V]{
			val:    val.val,
			expire: fastime.UnixNanoNow() + int64(d),
		}
		if shard.CompareAndSwap(key, val, newVal) {
			return newVal.val, true
		}
	}
}

// GetWithIgnoredExpire returns value & exists from key, ignoring expiration.
func (g *gache[V]) GetWithIgnoredExpire(key string) (v V, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].Load(key)
	if !ok {
		return v, false
	}
	return val.val, true
}

// Keys returns all keys in the Gache.
func (g *gache[V]) Keys(ctx context.Context) []string {
	keys := make([]string, 0, g.Len())
	mu := new(sync.Mutex)
	g.Range(ctx, func(key string, _ V, _ int64) bool {
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		return true
	})
	return keys
}

// Pop returns value & exists from key and deletes it.
func (g *gache[V]) Pop(key string) (v V, ok bool) {
	val, loaded := g.shards[getShardID(key, g.maxKeyLength)].LoadAndDelete(key)
	if !loaded {
		return v, false
	}
	atomic.AddUint64(&g.l, ^uint64(0))
	if val.isValid() {
		return val.val, true
	}
	if g.expFuncEnabled {
		g.expChan <- keyValue[V]{key: key, value: val.val}
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

	newVal := &value[V]{
		val:    val,
		expire: exp,
	}

	shard := g.shards[getShardID(key, g.maxKeyLength)]
	for {
		actual, loaded := shard.LoadOrStore(key, newVal)
		if !loaded {
			atomic.AddUint64(&g.l, 1)
			return
		}

		if actual.isValid() {
			return
		}

		if shard.CompareAndSwap(key, actual, newVal) {
			return
		}
	}
}
