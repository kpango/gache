// Package gache provides a high-performance, generic, concurrent-safe in-memory
// cache library for Go. It uses sharded storage to minimise lock contention and
// supports automatic expiration, expiration hooks, and serialisation to/from
// [io.Writer] and [io.Reader].
//
// Basic usage:
//
//	// Create a new string cache with default settings (30 second TTL).
//	gc := gache.New[string]()
//
//	// Store and retrieve a value.
//	gc.Set("greeting", "hello")
//	if v, ok := gc.Get("greeting"); ok {
//	    fmt.Println(v) // "hello"
//	}
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
	"golang.org/x/sync/errgroup"
)

type (
	// Gache is the primary interface for interacting with a gache cache instance.
	// The type parameter V constrains the value type stored in the cache, providing
	// compile-time type safety.
	//
	// All methods are safe for concurrent use by multiple goroutines.
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

	// gache is base instance type.
	gache[V any] struct {
		shards         [slen]*Map[string, value[V]]
		cancel         atomic.Pointer[context.CancelFunc]
		expChan        chan kv[V]
		expFunc        func(context.Context, string, V)
		valPool        *sync.Pool
		expFuncEnabled bool
		expire         int64
		maxKeyLength   uint64
		maxWorkers     int
	}

	value[V any] struct {
		mu     sync.RWMutex
		key    string
		val    V
		expire int64
	}

	kv[V any] struct {
		value V
		key   string
	}
)

const (
	// slen is shards length.
	slen = 4096
	// slen = 512
	// mask is slen-1 Hex value.
	mask = 0xFFF
	// mask = 0x1FF.

	// NoTTL can be used when setting a cache entry that should never expire.
	// Pass it as the expiration duration to [Gache.SetWithExpire] or
	// [WithDefaultExpiration] to disable time-based expiration for the entry
	// or for the entire cache, respectively.
	//
	// Example:
	//
	//	gc := gache.New[string]()
	//	gc.SetWithExpire("permanent", "value", gache.NoTTL)
	NoTTL time.Duration = -1
)

// hashSeed is initialized once at package load time and shared by all cache instances.
// This is an intentional design choice to avoid per-instance seed management overhead.
// If your threat model requires each cache instance to have a distinct hash seed for
// stronger isolation against collision attacks, do not assume per-instance seeding.
var hashSeed = maphash.MakeSeed()

// New creates and returns a new [Gache] instance parameterised over the value
// type V. By default the cache uses a 30-second TTL and a maximum key length
// of 256 bytes for shard selection. These defaults can be overridden by passing
// functional [Option] values.
//
// Example:
//
//	// Create a cache with default settings.
//	gc := gache.New[string]()
//
//	// Create a cache with a custom expiration and max key length.
//	gc = gache.New[string](
//	    gache.WithDefaultExpiration[string](time.Minute),
//	    gache.WithMaxKeyLength[string](128),
//	)
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
		WithMaxWorkers[V](runtime.NumCPU() * 2),
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

// isValid checks expiration of value.
func (v *value[V]) isValid(key string) (valid bool, match bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.key != key {
		return false, false
	}
	return v.expire <= 0 || fastime.UnixNanoNow() <= v.expire, true
}

// reset zeros out all fields to prevent memory leaks from retained references
// when the value object is returned to the pool for reuse.
func (v *value[V]) reset() {
	v.mu.Lock()
	var zero V
	v.key = ""
	v.val = zero
	v.expire = 0
	v.mu.Unlock()
}

// SetDefaultExpire sets the default expiration duration used by [Gache.Set] and
// other methods that do not accept an explicit TTL. The change takes effect
// immediately for all subsequent writes. It returns the receiver so calls can
// be chained.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetDefaultExpire(5 * time.Minute)
//	gc.Set("key", "value") // expires in 5 minutes
func (g *gache[V]) SetDefaultExpire(ex time.Duration) Gache[V] {
	atomic.StoreInt64(&g.expire, *(*int64)(unsafe.Pointer(&ex)))
	return g
}

// EnableExpiredHook enables the expired-entry hook so that the function
// registered via [Gache.SetExpiredHook] is called whenever a cached entry
// expires. The hook is invoked asynchronously by the daemon started with
// [Gache.StartExpired]. It returns the receiver for chaining.
//
// Example:
//
//	gc := gache.New[string]().
//	    SetExpiredHook(func(ctx context.Context, key string, val string) {
//	        fmt.Println("expired:", key)
//	    }).
//	    EnableExpiredHook().
//	    StartExpired(context.Background(), time.Minute)
func (g *gache[V]) EnableExpiredHook() Gache[V] {
	g.expFuncEnabled = true
	return g
}

// DisableExpiredHook disables the expired-entry hook. After this call the hook
// function registered via [Gache.SetExpiredHook] will no longer be invoked
// when entries expire. It returns the receiver for chaining.
//
// Example:
//
//	gc.DisableExpiredHook()
func (g *gache[V]) DisableExpiredHook() Gache[V] {
	g.expFuncEnabled = false
	return g
}

// SetExpiredHook registers a function that will be called asynchronously
// whenever a cached entry expires, provided the hook has been enabled via
// [Gache.EnableExpiredHook]. The function receives the context from
// [Gache.StartExpired], the key, and the expired value. It returns the
// receiver for chaining.
//
// Example:
//
//	gc := gache.New[int]().
//	    SetExpiredHook(func(ctx context.Context, key string, val int) {
//	        log.Printf("key %s (value %d) has expired", key, val)
//	    }).
//	    EnableExpiredHook().
//	    StartExpired(context.Background(), 30*time.Second)
func (g *gache[V]) SetExpiredHook(f func(context.Context, string, V)) Gache[V] {
	g.expFunc = f
	return g
}

// StartExpired starts a background goroutine (daemon) that periodically scans
// for and removes expired cache entries at the given interval dur. If an
// expired-entry hook has been enabled (see [Gache.EnableExpiredHook]), the hook
// function is also invoked for each expired entry. The daemon can be stopped by
// cancelling the provided context or by calling [Gache.Stop]. It returns the
// receiver for chaining.
//
// Example:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	gc := gache.New[string]().
//	    SetDefaultExpire(10 * time.Second).
//	    StartExpired(ctx, time.Minute) // sweep every minute
func (g *gache[V]) StartExpired(ctx context.Context, dur time.Duration) Gache[V] {
	go func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		g.cancel.Store(&cancel)
		tick := time.NewTicker(dur)
		eg, egctx := errgroup.WithContext(ctx)
		nprocs := g.numWorkers()
		eg.SetLimit(min(nprocs*2, g.maxWorkers))
		for {
			select {
			case <-egctx.Done():
				tick.Stop()
				eg.Wait()
				return
			case ex := <-g.expChan:
				eg.Go(func() error {
					g.expFunc(egctx, ex.key, ex.value)
					return nil
				})
			case <-tick.C:
				eg.Go(func() error {
					g.DeleteExpired(egctx)
					runtime.Gosched()
					return nil
				})
			}
		}
	}()
	return g
}

// ToMap returns all non-expired cache entries as a [*sync.Map]. Each key in the
// returned map is a string and each value has the cache's value type V. The
// operation can be cancelled via the provided context.
//
// Example:
//
//	gc := gache.New[int]()
//	gc.Set("a", 1)
//	gc.Set("b", 2)
//
//	m := gc.ToMap(context.Background())
//	m.Range(func(k, v any) bool {
//	    fmt.Printf("%s = %d\n", k, v)
//	    return true
//	})
func (g *gache[V]) ToMap(ctx context.Context) (m *sync.Map) {
	m = new(sync.Map)
	_ = g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		v.mu.RLock()
		if v.key == k {
			m.Store(k, v.val)
		}
		v.mu.RUnlock()
		return true
	})
	return m
}

func gatherChunks[V any, T any](g *gache[V], ctx context.Context, extract func(k string, v *value[V]) T) ([][]T, int) {
	numWorkers := g.numWorkers()

	var totalLen int
	for i := range slen {
		totalLen += g.shards[i].Len()
	}

	chunks := make([][]T, numWorkers)
	binLen := totalLen/numWorkers + 1
	for i := range numWorkers {
		chunks[i] = make([]T, 0, binLen)
	}

	_ = g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		v.mu.RLock()
		if v.key == k {
			item := extract(k, v)
			chunks[workerID] = append(chunks[workerID], item)
		}
		v.mu.RUnlock()
		return true
	})

	return chunks, totalLen
}

// ToRawMap returns all non-expired cache entries as a plain map[string]V. The
// operation can be cancelled via the provided context. Because the returned map
// is not synchronised, it should not be accessed concurrently without external
// locking.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("lang", "Go")
//
//	m := gc.ToRawMap(context.Background())
//	fmt.Println(m["lang"]) // "Go"
func (g *gache[V]) ToRawMap(ctx context.Context) (m map[string]V) {
	chunks, totalLen := gatherChunks(g, ctx, func(k string, v *value[V]) kv[V] {
		return kv[V]{key: k, value: v.val}
	})

	m = make(map[string]V, totalLen)
	for i := range chunks {
		for _, item := range chunks[i] {
			m[item.key] = item.value
		}
	}
	return m
}

// Keys returns a slice containing all keys of non-expired entries currently
// stored in the cache. The operation can be cancelled via the provided context.
//
// Example:
//
//	gc := gache.New[int]()
//	gc.Set("x", 1)
//	gc.Set("y", 2)
//
//	keys := gc.Keys(context.Background())
//	fmt.Println(keys) // e.g. ["x", "y"]
func (g *gache[V]) Keys(ctx context.Context) (keys []string) {
	chunks, totalLen := gatherChunks(g, ctx, func(k string, v *value[V]) string {
		return k
	})

	keys = make([]string, 0, totalLen)
	for i := range chunks {
		keys = append(keys, chunks[i]...)
	}
	return keys
}

// Values returns a slice containing all values of non-expired entries currently
// stored in the cache. The operation can be cancelled via the provided context.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("a", "alpha")
//	gc.Set("b", "beta")
//
//	vals := gc.Values(context.Background())
//	fmt.Println(vals) // e.g. ["alpha", "beta"]
func (g *gache[V]) Values(ctx context.Context) (values []V) {
	chunks, totalLen := gatherChunks(g, ctx, func(k string, v *value[V]) V {
		return v.val
	})

	values = make([]V, 0, totalLen)
	for i := range chunks {
		values = append(values, chunks[i]...)
	}
	return values
}

// get returns value & exists from key.
func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].LoadPointer(key)
	if !ok {
		return v, 0, false
	}

	val.mu.RLock()
	if val.key != key {
		val.mu.RUnlock()
		return v, 0, false
	}
	v = val.val
	expire = val.expire
	val.mu.RUnlock()

	if expire <= 0 || fastime.UnixNanoNow() <= expire {
		return v, expire, true
	}

	g.expiration(key)
	return v, expire, false
}

// Get retrieves the value associated with key. The second return value
// reports whether the key was found and the entry has not expired.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("color", "blue")
//
//	if v, ok := gc.Get("color"); ok {
//	    fmt.Println(v) // "blue"
//	}
func (g *gache[V]) Get(key string) (v V, ok bool) {
	v, _, ok = g.get(key)
	return v, ok
}

// GetWithExpire retrieves the value and its expiration unix-nano timestamp for
// key. The third return value reports whether the key was found and the entry
// has not expired. An expire value ≤ 0 indicates that the entry has no
// expiration (see [NoTTL]).
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("token", "abc123", 5*time.Minute)
//
//	if v, expire, ok := gc.GetWithExpire("token"); ok {
//	    remaining := time.Until(time.Unix(0, expire))
//	    fmt.Printf("value=%s remaining=%v\n", v, remaining)
//	}
func (g *gache[V]) GetWithExpire(key string) (v V, expire int64, ok bool) {
	return g.get(key)
}

// set sets key-value & expiration to Gache.
func (g *gache[V]) set(key string, val V, expire int64) {
	if expire > 0 {
		expire = fastime.UnixNanoNow() + expire
	}
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	newVal := g.valPool.Get().(*value[V])
	newVal.mu.Lock()
	newVal.key = key
	newVal.val = val
	newVal.expire = expire
	newVal.mu.Unlock()
	old, loaded := shard.SwapPointer(key, newVal)
	if loaded {
		old.reset()
		g.valPool.Put(old)
	}
}

// SetWithExpire stores the key-value pair with the given expiration duration.
// If expire is [NoTTL] (or any negative duration), the entry will never expire
// automatically.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("session", "sid_xyz", 30*time.Minute)
func (g *gache[V]) SetWithExpire(key string, val V, expire time.Duration) {
	g.set(key, val, *(*int64)(unsafe.Pointer(&expire)))
}

// Set stores the key-value pair using the cache's default expiration duration
// (set via [Gache.SetDefaultExpire] or [WithDefaultExpiration], defaults to
// 30 seconds).
//
// Example:
//
//	gc := gache.New[int]()
//	gc.Set("count", 42)
func (g *gache[V]) Set(key string, val V) {
	g.set(key, val, atomic.LoadInt64(&g.expire))
}

// Delete removes the entry for key from the cache and returns the value that
// was stored along with a boolean indicating whether the key was present.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("tmp", "data")
//
//	if v, ok := gc.Delete("tmp"); ok {
//	    fmt.Println("deleted:", v) // "deleted: data"
//	}
func (g *gache[V]) Delete(key string) (v V, loaded bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded := shard.LoadAndDeletePointer(key)
	if loaded {
		val.mu.RLock()
		if val.key != key {
			val.mu.RUnlock()
			return v, false
		}
		v = val.val
		val.mu.RUnlock()
		val.reset()
		g.valPool.Put(val)
		return v, true
	}
	return v, false
}

func (g *gache[V]) expiration(key string) {
	v, loaded := g.Delete(key)
	if loaded && g.expFuncEnabled {
		g.expChan <- kv[V]{key: key, value: v}
	}
}

// DeleteExpired scans the entire cache and removes all entries whose
// expiration time has passed. It returns the number of entries deleted. The
// operation can be cancelled early via the provided context.
//
// Note: If [Gache.StartExpired] is running, expired entries are already
// cleaned up periodically, so calling DeleteExpired manually is usually
// unnecessary.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("short", "v", time.Millisecond)
//	time.Sleep(5 * time.Millisecond)
//
//	n := gc.DeleteExpired(context.Background())
//	fmt.Printf("removed %d expired entries\n", n)
func (g *gache[V]) DeleteExpired(ctx context.Context) (expired uint64) {
	return g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		return true
	})
}

// Range iterates over every non-expired entry in the cache, calling f for each
// one. The iteration stops early when f returns false or when the context is
// cancelled. The function f receives the key, value, and expiration timestamp
// (unix nanoseconds). It returns the receiver for chaining.
//
// Example:
//
//	gc := gache.New[int]()
//	gc.Set("a", 1)
//	gc.Set("b", 2)
//
//	gc.Range(context.Background(), func(key string, val int, exp int64) bool {
//	    fmt.Printf("%s -> %d\n", key, val)
//	    return true // continue iteration
//	})
func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	_ = g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		v.mu.RLock()
		if v.key != k {
			v.mu.RUnlock()
			return true
		}
		val := v.val
		exp := v.expire
		v.mu.RUnlock()
		return f(k, val, exp)
	})
	return g
}

func (g *gache[V]) numWorkers() int {
	// If maxWorkers is zero or negative, disable concurrency by using a single worker.
	if g.maxWorkers <= 0 {
		return 1
	}

	nprocs := min(runtime.GOMAXPROCS(0), g.maxWorkers)
	return nprocs
}

func (g *gache[V]) loop(ctx context.Context, f func(int, string, *value[V]) bool) (expiredRows uint64) {
	nprocs := g.numWorkers()
	if slenInt := int(slen); nprocs > slenInt {
		nprocs = slenInt
	}
	var fn func(workerID int, k string, v *value[V]) bool
	if f != nil {
		fn = func(workerID int, k string, v *value[V]) bool {
			if v != nil {
				valid, match := v.isValid(k)
				if !match {
					return true
				}
				if !valid {
					g.expiration(k)
					atomic.AddUint64(&expiredRows, 1)
				} else {
					return f(workerID, k, v)
				}
			}
			return true
		}
	} else {
		fn = func(_ int, k string, v *value[V]) bool {
			if v != nil {
				valid, match := v.isValid(k)
				if match && !valid {
					g.expiration(k)
					atomic.AddUint64(&expiredRows, 1)
				}
			}
			return true
		}
	}

	if nprocs <= 1 {
		for i := range slen {
			if ctx.Err() != nil {
				break
			}
			g.shards[i].RangePointer(func(k string, v *value[V]) bool { return fn(0, k, v) })
		}
		return atomic.LoadUint64(&expiredRows)
	}

	var idx atomic.Uint64
	var wg sync.WaitGroup
	worker := func(workerID int) {
		for {
			endIdx := idx.Add(16)
			startIdx := endIdx - 16
			if startIdx >= slen || ctx.Err() != nil {
				wg.Done()
				return
			}
			if endIdx > slen {
				endIdx = slen
			}
			for j := startIdx; j < endIdx; j++ {
				g.shards[j].RangePointer(func(k string, v *value[V]) bool { return fn(workerID, k, v) })
			}
		}
	}

	wg.Add(nprocs)
	for i := range nprocs - 1 {
		go worker(i)
	}
	worker(nprocs - 1)
	wg.Wait()
	return atomic.LoadUint64(&expiredRows)
}

// Len returns the total number of entries (including possibly expired but not
// yet cleaned up entries) currently stored in the cache.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("a", "1")
//	gc.Set("b", "2")
//	fmt.Println(gc.Len()) // 2
func (g *gache[V]) Len() (l int) {
	for i := range g.shards {
		l += g.shards[i].Len()
	}
	return l
}

// Size returns an approximate in-memory size of the cache in bytes. The
// returned value includes the fixed overhead of the gache struct fields as well
// as the size reported by each internal shard.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("k", "v")
//	fmt.Printf("cache size: %d bytes\n", gc.Size())
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

// Write serialises all non-expired cache entries to w using encoding/gob.
// The operation can be cancelled via the provided context. The data written by
// Write can later be restored with [Gache.Read].
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("key", "value")
//
//	var buf bytes.Buffer
//	if err := gc.Write(context.Background(), &buf); err != nil {
//	    log.Fatal(err)
//	}
func (g *gache[V]) Write(ctx context.Context, w io.Writer) error {
	m := g.ToRawMap(ctx)
	gob.Register(map[string]V{})
	return gob.NewEncoder(w).Encode(&m)
}

// Read deserialises cache entries from r (previously written by [Gache.Write])
// and stores them in the cache using the current default expiration. Entries
// are inserted in parallel using worker goroutines, but Read blocks until all
// insertions are complete before returning.
//
// Example:
//
//	gc := gache.New[string]()
//	file, _ := os.Open("cache.gob")
//	defer file.Close()
//
//	if err := gc.Read(file); err != nil {
//	    log.Fatal(err)
//	}
func (g *gache[V]) Read(r io.Reader) error {
	var m map[string]V
	gob.Register(map[string]V{})
	err := gob.NewDecoder(r).Decode(&m)
	if err != nil {
		return err
	}

	sizePerShard := len(m) / slen
	if sizePerShard > 0 {
		for i := range slen {
			g.shards[i].InitReserve(sizePerShard)
		}
	}

	var wg sync.WaitGroup

	numWorkers := g.numWorkers()

	chunks := make([][]kv[V], numWorkers)

	for i := range chunks {
		chunks[i] = make([]kv[V], 0, len(m)/numWorkers+1)
	}

	i := 0
	for k, v := range m {
		chunks[i%numWorkers] = append(chunks[i%numWorkers], kv[V]{key: k, value: v})
		i++
	}

	wg.Add(numWorkers)
	for i := range numWorkers {
		go func(chunk []kv[V]) {
			defer wg.Done()
			for _, item := range chunk {
				g.Set(item.key, item.value)
			}
		}(chunks[i])
	}
	wg.Wait()
	return nil
}

// Stop cancels the background expiration daemon started by [Gache.StartExpired].
// After Stop returns, no further automatic expiration sweeps or hook invocations
// will occur.
//
// Example:
//
//	gc := gache.New[string]().
//	    StartExpired(context.Background(), time.Minute)
//	// ... use the cache ...
//	gc.Stop() // shut down the expiration daemon
func (g *gache[V]) Stop() {
	if c := g.cancel.Load(); c != nil {
		cancel := *c
		cancel()
	}
}

// Clear removes all entries from the cache, effectively resetting it to an
// empty state. This is an O(shards) operation and does not stop the expiration
// daemon if one is running.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("a", "1")
//	gc.Set("b", "2")
//	gc.Clear()
//	fmt.Println(gc.Len()) // 0
func (g *gache[V]) Clear() {
	for i := range g.shards {
		if g.shards[i] == nil {
			g.shards[i] = newMap[V]()
		} else {
			g.shards[i].Clear()
		}
	}
}

// ExtendExpire extends the expiration of an existing non-expired entry by
// addExp. If the key does not exist or has already expired, ExtendExpire is a
// no-op.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("sess", "data", 10*time.Minute)
//
//	// User activity detected — extend the session by another 10 minutes.
//	gc.ExtendExpire("sess", 10*time.Minute)
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
		valid, match := val.isValid(key)
		if !match {
			continue
		}
		if !valid {
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

		var copied bool
		val.mu.RLock()
		if val.key == key {
			newVal.mu.Lock()
			newVal.key = key
			newVal.val = val.val
			newVal.expire = val.expire + int64(addExp)
			newVal.mu.Unlock()
			copied = true
		}
		val.mu.RUnlock()

		if !copied {
			continue
		}

		if shard.CompareAndSwapPointer(key, val, newVal) {
			val.reset()
			g.valPool.Put(val)
			return
		}
	}
}

// GetRefresh retrieves the value for key and, if the entry exists and has not
// expired, resets its expiration to the cache's default TTL. This is equivalent
// to calling GetRefreshWithDur with the default expiration duration.
//
// Example:
//
//	gc := gache.New[string]().SetDefaultExpire(5 * time.Minute)
//	gc.Set("token", "abc")
//
//	// Each access refreshes the TTL back to 5 minutes.
//	if v, ok := gc.GetRefresh("token"); ok {
//	    fmt.Println(v) // "abc"
//	}
func (g *gache[V]) GetRefresh(key string) (V, bool) {
	return g.GetRefreshWithDur(key, time.Duration(atomic.LoadInt64(&g.expire)))
}

// GetRefreshWithDur retrieves the value for key and, if the entry exists and
// has not expired, resets its expiration to d from now. This is useful for
// implementing sliding-window TTL patterns.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("sess", "user1", 10*time.Minute)
//
//	// Refresh with a custom duration of 15 minutes.
//	if v, ok := gc.GetRefreshWithDur("sess", 15*time.Minute); ok {
//	    fmt.Println("session:", v) // "session: user1"
//	}
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
		valid, match := val.isValid(key)
		if !match {
			continue
		}
		if !valid {
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

		var copied bool
		val.mu.RLock()
		if val.key == key {
			newVal.mu.Lock()
			newVal.key = key
			newVal.val = val.val
			newVal.expire = fastime.UnixNanoNow() + int64(d)
			newVal.mu.Unlock()
			v = newVal.val
			copied = true
		}
		val.mu.RUnlock()

		if !copied {
			continue
		}

		if shard.CompareAndSwapPointer(key, val, newVal) {
			val.reset()
			g.valPool.Put(val)
			return v, true
		}
	}
}

// GetWithIgnoredExpire retrieves the value for key regardless of whether the
// entry has expired. The second return value reports only whether the key
// exists in the cache, not whether it is still valid. This can be useful for
// diagnostics or stale-while-revalidate patterns.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("k", "v", time.Millisecond)
//	time.Sleep(5 * time.Millisecond) // entry is now expired
//
//	if v, ok := gc.GetWithIgnoredExpire("k"); ok {
//	    fmt.Println("stale value:", v)
//	}
func (g *gache[V]) GetWithIgnoredExpire(key string) (v V, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].LoadPointer(key)
	if !ok {
		return v, false
	}
	val.mu.RLock()
	if val.key != key {
		val.mu.RUnlock()
		return v, false
	}
	v = val.val
	val.mu.RUnlock()
	return v, true
}

// Pop atomically retrieves and removes the entry for key. It returns the value
// and true if the key existed and had not expired; otherwise it returns the
// zero value and false. If the entry had expired and an expired hook is
// enabled, the hook will be triggered.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("job", "payload")
//
//	if v, ok := gc.Pop("job"); ok {
//	    fmt.Println("processing:", v) // "processing: payload"
//	}
//	// "job" is no longer in the cache.
func (g *gache[V]) Pop(key string) (v V, ok bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded := shard.LoadAndDeletePointer(key)
	if !loaded {
		return v, false
	}
	val.mu.RLock()
	if val.key != key {
		val.mu.RUnlock()
		return v, false
	}
	v = val.val
	valid := val.expire <= 0 || fastime.UnixNanoNow() <= val.expire
	val.mu.RUnlock()
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

// SetIfNotExists stores the key-value pair only if key is not already present
// (or has expired) in the cache. The default expiration is used. This provides
// a simple compare-and-set semantic for cache population.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetIfNotExists("init", "first")
//	gc.SetIfNotExists("init", "second") // no-op, "init" already exists
//
//	v, _ := gc.Get("init")
//	fmt.Println(v) // "first"
func (g *gache[V]) SetIfNotExists(key string, val V) {
	g.SetWithExpireIfNotExists(key, val, time.Duration(atomic.LoadInt64(&g.expire)))
}

// SetWithExpireIfNotExists stores the key-value pair with a custom expiration
// duration d, but only if key is not already present (or has expired) in the
// cache.
//
// Example:
//
//	gc := gache.New[int]()
//	gc.SetWithExpireIfNotExists("counter", 1, 10*time.Minute)
//	gc.SetWithExpireIfNotExists("counter", 999, 10*time.Minute) // no-op
//
//	v, _ := gc.Get("counter")
//	fmt.Println(v) // 1
func (g *gache[V]) SetWithExpireIfNotExists(key string, val V, d time.Duration) {
	exp := int64(d)
	if exp > 0 {
		exp += fastime.UnixNanoNow()
	}

	newVal := g.valPool.Get().(*value[V])
	newVal.mu.Lock()
	newVal.key = key
	newVal.val = val
	newVal.expire = exp
	newVal.mu.Unlock()

	shard := g.shards[getShardID(key, g.maxKeyLength)]
	for {
		actual, loaded := shard.LoadOrStorePointer(key, newVal)
		if !loaded {
			return
		}

		// loaded: actual is the existing value (*value[V])

		valid, match := actual.isValid(key)
		if !match {
			continue
		}
		if valid {
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
