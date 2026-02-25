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
	}

	// gache is base instance type
	gache[V any] struct {
		shards         [slen]*Map[V]
		cancel         atomic.Pointer[context.CancelFunc]
		// expChan stores pointers to reused keyValue structs
		expChan        chan *keyValue[V]
		expFunc        func(context.Context, string, V)
		kvPool         sync.Pool
		expFuncEnabled bool
		expire         int64
		l              uint64
		maxKeyLength   uint64
		expireCursor   uint64 // For incremental expiration
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

// hashSeed is initialized once at package load time and shared by all cache instances.
var hashSeed = maphash.MakeSeed()

// now is a cached timestamp updated every 100ms
var now int64

func init() {
	atomic.StoreInt64(&now, fastime.UnixNanoNow())
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for t := range ticker.C {
			atomic.StoreInt64(&now, t.UnixNano())
		}
	}()
}

// Now returns the cached atomic timestamp
func Now() int64 {
	return atomic.LoadInt64(&now)
}

// New returns Gache (*gache) instance
func New[V any](opts ...Option[V]) Gache[V] {
	g := new(gache[V])
	g.kvPool = sync.Pool{
		New: func() any {
			return new(keyValue[V])
		},
	}
	for _, opt := range append([]Option[V]{
		WithMaxKeyLength[V](256),
	}, opts...) {
		opt(g)
	}
	g.Clear() // Initialize shards
	g.expChan = make(chan *keyValue[V], len(g.shards)*10)
	return g
}

func newMap[V any]() (m *Map[V]) {
	return NewMap[V]()
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
				k, v := kv.key, kv.value
				// Return to pool immediately
				g.kvPool.Put(kv)

				// Execute callback
				go g.expFunc(ctx, k, v)

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
	shard := g.shards[getShardID(key, g.maxKeyLength)]

	// Use cached time to avoid syscall overhead
	now := Now()
	v, expire, ok = shard.Load(key, now)
	if !ok {
		return v, 0, false
	}

	if expire > 0 && expire < now {
		g.expiration(key)
		return v, expire, false
	}

	return v, expire, true
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
		expire = Now() + expire
	}
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	isNew := shard.Store(key, val, expire)
	if isNew {
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
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	v, loaded = shard.Delete(key)
	if loaded {
		atomic.AddUint64(&g.l, ^uint64(0))
	}
	return v, loaded
}

func (g *gache[V]) expiration(key string) {
	v, loaded := g.Delete(key)

	if loaded && g.expFuncEnabled {
		// Use sync.Pool for keyValue struct
		kv := g.kvPool.Get().(*keyValue[V])
		kv.key = key
		kv.value = v

		select {
		case g.expChan <- kv:
		default:
			// Buffer full, drop event and return to pool
			g.kvPool.Put(kv)
		}
	}
}

// DeleteExpired deletes expired value from Gache it can be cancel using context
func (g *gache[V]) DeleteExpired(ctx context.Context) (rows uint64) {
	// Incremental Eviction: Scan a subset of shards
	const batchSize = 10
	startCursor := atomic.LoadUint64(&g.expireCursor)
	nextCursor := (startCursor + batchSize) % slen
	atomic.StoreUint64(&g.expireCursor, nextCursor)

	n := Now()

	var wg sync.WaitGroup
	for i := 0; i < batchSize; i++ {
		idx := (startCursor + uint64(i)) % slen
		wg.Add(1)
		go func(c context.Context, shardIdx int) {
			defer wg.Done()
			select {
			case <-c.Done():
				return
			default:
				// Execute Timing Wheel eviction on this shard
				count := g.shards[shardIdx].EvictExpired(n, func(k string, v V) {
					if g.expFuncEnabled {
						// Allocate from pool
						kv := g.kvPool.Get().(*keyValue[V])
						kv.key = k
						kv.value = v

						select {
						case g.expChan <- kv:
						default:
							g.kvPool.Put(kv)
						}
					}
				})
				atomic.AddUint64(&rows, count)
			}
		}(ctx, int(idx))
	}
	wg.Wait()

	if rows > 0 {
		atomic.AddUint64(&g.l, uint64(0) - rows)
	}

	return rows
}

// Range calls f sequentially for each key and value present in the Gache.
func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	now := Now()
	wg := new(sync.WaitGroup)
	for i := range g.shards {
		wg.Add(1)
		go func(c context.Context, idx int) {
			defer wg.Done()
			select {
			case <-c.Done():
				return
			default:
				g.shards[idx].Range(func(k string, v V, exp int64) (ok bool) {
					if exp <= 0 || exp > now {
						return f(k, v, exp)
					}
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
	atomic.StoreUint64(&g.l, 0)
}
