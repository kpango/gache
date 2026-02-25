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
		shards         [slen]*Map[string, value[V]]
		timer          *timingWheel
		clock          *Clock
		cancel         atomic.Pointer[context.CancelFunc]
		expChan        chan *keyValue[V]
		kvPool         *sync.Pool
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
	// mask is slen-1 Hex value
	mask = 0x1FF

	// NoTTL can be use for disabling ttl cache expiration
	NoTTL time.Duration = -1
)

var (
	// hashSeed is used for maphash
	hashSeed = maphash.MakeSeed()
)

// New creates a new Gache instance
func New[V any](opts ...Option[V]) Gache[V] {
	clock := NewClock(100 * time.Millisecond)
	g := &gache[V]{
		expire:       int64(NoTTL),
		maxKeyLength: 256,
		kvPool: &sync.Pool{
			New: func() any {
				return new(keyValue[V])
			},
		},
		timer: newTimingWheel(clock.Now()),
		clock: clock,
	}
	for i := range g.shards {
		g.shards[i] = newMap[string, value[V]]()
	}
	for _, opt := range opts {
		if err := opt(g); err != nil {
			panic(err)
		}
	}
	// Initialize expChan with buffer size
	g.expChan = make(chan *keyValue[V], len(g.shards)*10)
	return g
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
func (v value[V]) isValid(now int64) (valid bool) {
	return v.expire <= 0 || now <= v.expire
}

// SetDefaultExpire set expire duration
func (g *gache[V]) SetDefaultExpire(ex time.Duration) Gache[V] {
	atomic.StoreInt64(&g.expire, int64(ex))
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

// StartExpired starts delete expired value daemon using Timing Wheel
func (g *gache[V]) StartExpired(ctx context.Context, dur time.Duration) Gache[V] {
	go func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		g.cancel.Store(&cancel)
		tick := time.NewTicker(dur)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case kv := <-g.expChan:
				go func(kv *keyValue[V]) {
					g.expFunc(ctx, kv.key, kv.value)
					// Return to pool
					*kv = keyValue[V]{} // Zero out
					g.kvPool.Put(kv)
				}(kv)
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
	now := g.clock.Now()
	for i := range g.shards {
		select {
		case <-ctx.Done():
			return m
		default:
			g.shards[i].Range(func(k string, v value[V]) bool {
				if v.isValid(now) {
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
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, ok := shard.Load(key)
	if !ok {
		return v, 0, false
	}

	if val.isValid(g.clock.Now()) {
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
		expire = g.clock.Now() + expire
		// Add to timing wheel
		g.timer.add(key, expire)
	}
	shard := g.shards[getShardID(key, g.maxKeyLength)]

	_, loaded := shard.Swap(key, value[V]{
		expire: expire,
		val:    val,
	})

	if !loaded {
		atomic.AddUint64(&g.l, 1)
	}
}

// SetWithExpire sets key-value & expiration to Gache
func (g *gache[V]) SetWithExpire(key string, val V, expire time.Duration) {
	g.set(key, val, int64(expire))
}

// Set sets key-value to Gache using default expiration
func (g *gache[V]) Set(key string, val V) {
	g.set(key, val, atomic.LoadInt64(&g.expire))
}

// Delete deletes value from Gache using key
func (g *gache[V]) Delete(key string) (v V, loaded bool) {
	var val value[V]
	val, loaded = g.shards[getShardID(key, g.maxKeyLength)].LoadAndDelete(key)
	if loaded {
		atomic.AddUint64(&g.l, ^uint64(0))
		return val.val, loaded
	}
	return v, loaded
}

func (g *gache[V]) expiration(key string) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, ok := shard.Load(key)
	if !ok {
		return
	}
	// Check if expired
	if val.isValid(g.clock.Now()) {
		return
	}

	// Compare and Delete
	if shard.CompareAndDelete(key, val) {
		atomic.AddUint64(&g.l, ^uint64(0))
		if g.expFuncEnabled {
			// Get from pool
			kv := g.kvPool.Get().(*keyValue[V])
			kv.key = key
			kv.value = val.val

			// Non-blocking send
			select {
			case g.expChan <- kv:
			default:
				*kv = keyValue[V]{}
				g.kvPool.Put(kv)
			}
		}
	}
}

// DeleteExpired deletes expired value from Gache.
func (g *gache[V]) DeleteExpired(ctx context.Context) (rows uint64) {
	// Advance timing wheel
	now := g.clock.Now()
	keys := g.timer.advance(now)

	for _, key := range keys {
		select {
		case <-ctx.Done():
			return atomic.LoadUint64(&rows)
		default:
			shard := g.shards[getShardID(key, g.maxKeyLength)]
			val, ok := shard.Load(key)
			if !ok {
				continue
			}
			if !val.isValid(now) {
				if shard.CompareAndDelete(key, val) {
					atomic.AddUint64(&g.l, ^uint64(0))
					atomic.AddUint64(&rows, 1)
					if g.expFuncEnabled {
						kv := g.kvPool.Get().(*keyValue[V])
						kv.key = key
						kv.value = val.val
						select {
						case g.expChan <- kv:
						default:
							*kv = keyValue[V]{}
							g.kvPool.Put(kv)
						}
					}
				}
			}
		}
	}
	return atomic.LoadUint64(&rows)
}

// Range calls f sequentially for each key and value present in the Gache.
func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	wg := new(sync.WaitGroup)
	now := g.clock.Now()
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			select {
			case <-c.Done():
				return
			default:
				g.shards[idx].Range(func(k string, v value[V]) (ok bool) {
					if v.isValid(now) {
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
	// clock size?
	size += unsafe.Sizeof(g.clock)
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

// Stop stops the gache instance, including the clock and expire daemon.
func (g *gache[V]) Stop() {
	if g.clock != nil {
		g.clock.Stop()
	}
	if c := g.cancel.Load(); c != nil {
		cancel := *c
		cancel()
	}
}

// Clear deletes all key and value present in the Gache.
func (g *gache[V]) Clear() {
	for i := range g.shards {
		if g.shards[i] == nil {
			g.shards[i] = newMap[string, value[V]]()
		} else {
			g.shards[i].Clear()
		}
	}
}

func (v value[V]) Size() (size uintptr) {
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
		if !val.isValid(g.clock.Now()) {
			g.expiration(key)
			return
		}

		newVal := value[V]{
			val:    val.val,
			expire: val.expire + int64(addExp),
		}
		if shard.CompareAndSwap(key, val, newVal) {
			g.timer.add(key, newVal.expire)
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
		if !val.isValid(g.clock.Now()) {
			g.expiration(key)
			return v, false
		}

		newVal := value[V]{
			val:    val.val,
			expire: g.clock.Now() + int64(d),
		}
		if shard.CompareAndSwap(key, val, newVal) {
			g.timer.add(key, newVal.expire)
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
	if val.isValid(g.clock.Now()) {
		return val.val, true
	}
	if g.expFuncEnabled {
		kv := g.kvPool.Get().(*keyValue[V])
		kv.key = key
		kv.value = val.val
		select {
		case g.expChan <- kv:
		default:
			*kv = keyValue[V]{}
			g.kvPool.Put(kv)
		}
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
		exp += g.clock.Now()
	}

	newVal := value[V]{
		val:    val,
		expire: exp,
	}

	shard := g.shards[getShardID(key, g.maxKeyLength)]
	for {
		actual, loaded := shard.LoadOrStore(key, newVal)
		if !loaded {
			atomic.AddUint64(&g.l, 1)
			if exp > 0 {
				g.timer.add(key, exp)
			}
			return
		}

		if actual.isValid(g.clock.Now()) {
			return
		}

		if shard.CompareAndSwap(key, actual, newVal) {
			if exp > 0 {
				g.timer.add(key, exp)
			}
			return
		}
	}
}
