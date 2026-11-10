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
		shards         [slen]*shardMap[string, V]
		cancel         atomic.Pointer[context.CancelFunc]
		expChan        chan kv[V]
		expFunc        atomic.Pointer[func(context.Context, string, V)]
		expire         int64
		maxKeyLength   uint64
		maxWorkers     int
		expOnce        sync.Once
		expFuncEnabled atomic.Bool
	}

	kv[V any] struct {
		value V
		key   string
	}
)

const (
	// slen is shards length.
	slen = 4096
	// mask is slen-1 Hex value.
	mask = 0xFFF

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

// hashSeed is initialized once at package load time and shared by all cache instances
// and by every [Map], which hashes its keys with it as well.
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
	arena := make([]shardMap[string, V], slen)
	for i := range g.shards {
		g.shards[i] = &arena[i]
	}
	g.expire = (30 * time.Second).Nanoseconds()
	g.maxKeyLength = 256
	g.maxWorkers = runtime.NumCPU() * 2
	for _, opt := range opts {
		_ = opt(g)
	}
	return g
}

func (g *gache[V]) initExpChan() chan kv[V] {
	g.expOnce.Do(func() {
		g.expChan = make(chan kv[V], len(g.shards)*10)
	})
	return g.expChan
}

// getShardID returns the full 64-bit hash of key. The shard index is h & mask;
// the same h is used as the shardMap key.
func getShardID(key string, kl uint64) uint64 {
	lk := uint64(len(key))
	if kl != 0 && lk > kl {
		return hashTrimmed(key, kl)
	}
	if lk <= 32 {
		return hashShort(key, lk)
	}
	return hashLongKey(key, lk)
}

func hashShort(key string, lk uint64) uint64 {
	if lk == 0 {
		return 0
	}
	if lk == 1 {
		return uint64(key[0])
	}
	return maphash.String(hashSeed, key)
}

func hashTrimmed(key string, kl uint64) uint64 {
	return getShardID(key[:kl], 0)
}

func hashLongKey(key string, lk uint64) uint64 {
	if lk <= 64 {
		return hashMid(key)
	}
	ptr := unsafe.Pointer(unsafe.StringData(key))
	q1 := uintptr(lk >> 2)
	q3 := uintptr((lk * 3) >> 2)
	last := uintptr(lk - 16)

	var buf [64]byte
	bPtr := unsafe.Pointer(&buf[0])
	*(*[2]uint64)(bPtr) = *(*[2]uint64)(ptr)
	*(*[2]uint64)(unsafe.Add(bPtr, 16)) = *(*[2]uint64)(unsafe.Add(ptr, q1))
	*(*[2]uint64)(unsafe.Add(bPtr, 32)) = *(*[2]uint64)(unsafe.Add(ptr, q3))
	*(*[2]uint64)(unsafe.Add(bPtr, 48)) = *(*[2]uint64)(unsafe.Add(ptr, last))
	*(*uint64)(bPtr) ^= lk
	return hashSampled(&buf)
}

// shard returns the shard and full hash for key.
func (g *gache[V]) shard(key string) (*shardMap[string, V], uint64) {
	h := getShardID(key, g.maxKeyLength)
	return g.shards[h&mask], h
}

// SetDefaultExpire sets the default expiration duration used by [Gache.Set] and
// other methods that do not accept an explicit TTL. The change takes effect
// immediately for all subsequent writes. An ex of zero or less (such as [NoTTL])
// makes them never expire. It returns the receiver so calls can be chained.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetDefaultExpire(5 * time.Minute)
//	gc.Set("key", "value") // expires in 5 minutes
func (g *gache[V]) SetDefaultExpire(ex time.Duration) Gache[V] {
	atomic.StoreInt64(&g.expire, int64(ex))
	return g
}

// EnableExpiredHook enables the expired-entry hook: from now on an entry that is
// evicted because it has expired is queued for the function registered via
// [Gache.SetExpiredHook], which the daemon started by [Gache.StartExpired] calls
// asynchronously. If no function is registered, the daemon discards the entries it
// is handed and calls nothing. It returns the receiver for chaining.
//
// The queue belongs to the cache and holds 40960 entries (10 per shard). It is
// filled by the sweep of the daemon and by [Gache.DeleteExpired], and by the calls
// that find an entry expired and evict it: [Gache.Get], [Gache.GetWithExpire],
// [Gache.GetRefresh], [Gache.GetRefreshWithDur], [Gache.ExtendExpire],
// [Gache.Pop], [Gache.Range] and [Gache.ToMap]. A sweep waits for room when the
// queue is full. Those calls never wait: when the queue is full (it can stay full
// while no daemon runs, after [Gache.Stop], and while the hooks are slow) they
// drop the notification, and the entry is evicted all the same. A hook can
// therefore miss an entry that one of those calls evicted, and none of those
// calls blocks on the hooks, whatever they do; only a sweep and
// [Gache.DeleteExpired] wait for the queue. Replacing an expired entry with
// [Gache.SetWithExpireIfNotExists] or overwriting it with [Gache.Set] queues
// nothing, and neither does [Gache.Delete]; [Gache.Keys], [Gache.Values],
// [Gache.ToRawMap], [Gache.Write] and [Gache.GetWithIgnoredExpire] leave expired
// entries in place.
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
	g.expFuncEnabled.Store(true)
	g.initExpChan()
	return g
}

// DisableExpiredHook disables the expired-entry hook: entries evicted from now on
// are no longer queued. Entries that are already queued stay queued, and the
// daemon serves them as long as a function is registered via
// [Gache.SetExpiredHook]. It returns the receiver for chaining.
//
// Example:
//
//	gc.DisableExpiredHook()
func (g *gache[V]) DisableExpiredHook() Gache[V] {
	g.expFuncEnabled.Store(false)
	return g
}

// SetExpiredHook registers the function the daemon calls for each queued expired
// entry, provided the hook has been enabled via [Gache.EnableExpiredHook]. The
// function receives the daemon's context (see [Gache.StartExpired]), the key and
// the expired value. The daemon runs several hooks at once, so f must be safe for
// concurrent use. SetExpiredHook may be called while a daemon runs: the new
// function serves the entries the daemon takes off the queue from then on, and a
// hook that is already running is not affected. A nil f leaves no function: the
// daemon then discards the entries it takes. It returns the receiver for
// chaining.
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
	var fp *func(context.Context, string, V)
	if f != nil {
		fp = &f
	}
	g.expFunc.Store(fp)
	g.initExpChan()
	return g
}

// StartExpired starts a background daemon of two goroutines. One deletes the
// expired entries every dur (see [Gache.DeleteExpired]). The other serves the
// expired-entry hook (see [Gache.EnableExpiredHook]): it calls the function set by
// [Gache.SetExpiredHook] for each queued entry, several at a time (the limit is
// described at [WithMaxWorkers]), with the daemon's context. A dur of zero or less
// starts no sweeper, since a ticker needs a positive interval: expired entries are
// then evicted only by the calls that find them and by explicit
// [Gache.DeleteExpired] calls, and the hook server still runs. The daemon stops
// when the provided context is cancelled or [Gache.Stop] is called. It returns the
// receiver for chaining.
//
// A cache has at most one daemon: calling StartExpired again cancels the daemon an
// earlier call started (whatever dur is), so that [Gache.Stop] always reaches the
// running one. The earlier daemon winds down asynchronously: StartExpired does not
// wait for it, a hook it is running keeps running with its cancelled context, and
// an entry it takes off the queue before it notices the cancellation is served
// with that cancelled context as well.
//
// The queue belongs to the cache, not to a daemon: entries queued while no daemon
// runs, or after one has stopped, stay queued and the next daemon serves them.
//
// The sweeps and the hooks run on separate goroutines. A sweep that finds more
// expired entries than the queue holds waits for the hooks to drain it, so a slow
// hook slows the sweeps down. Apart from [Gache.DeleteExpired], the calls that
// evict an expired entry never wait for the queue (see [Gache.EnableExpiredHook]),
// so a hook may call them. A hook that never returns holds one of the hook slots
// for good; once every slot is held, the hook server stalls and, when the queue
// is full, so do the sweeps, and such hooks keep the daemon from winding down
// after it is stopped. A hook that calls [Gache.DeleteExpired] holds its slot
// while it waits for room in the same queue; with the daemon's context it gives
// up when the daemon stops, with a context that is never cancelled it can wait
// for good.
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
	g.initExpChan()
	// The cancel func is published before the goroutines start, so a Stop right
	// after this call cannot miss the daemon.
	ctx, cancel := context.WithCancel(ctx)
	if prev := g.cancel.Swap(&cancel); prev != nil {
		(*prev)()
	}
	if dur > 0 {
		go g.sweepExpired(ctx, dur)
	}
	go g.serveExpired(ctx)
	return g
}

// sweepExpired deletes the expired entries every dur (> 0) until ctx is done. The
// sweep gets ctx too, so one blocked on a full expChan gives up when the daemon
// stops.
func (g *gache[V]) sweepExpired(ctx context.Context, dur time.Duration) {
	tick := time.NewTicker(dur)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			g.DeleteExpired(ctx)
		}
	}
}

// serveExpired runs the expired hook for every entry queued on expChan, hookWorkers
// at a time, until ctx is done, then waits for the hooks still running. It is the
// daemon's only receiver of expChan and waits only for a hook, never for the sweep.
// The sweep waits for it, and the calls a hook may make that evict an expired entry
// never wait for expChan, so the queue drains as long as the hooks return. A hook
// that waits for room in expChan itself (it calls DeleteExpired) holds its errgroup
// slot until its own ctx is done; only the daemon's ctx is cancelled by Stop, so a
// hook passing another ctx can keep the wind-down below waiting.
func (g *gache[V]) serveExpired(ctx context.Context) {
	var hooks errgroup.Group
	hooks.SetLimit(g.hookWorkers())
	for {
		select {
		case <-ctx.Done():
			hooks.Wait() // the hooks return nil
			return
		case ex := <-g.expChan:
			// EnableExpiredHook without SetExpiredHook leaves nothing to call.
			if f := g.expFunc.Load(); f != nil {
				hooks.Go(func() error {
					(*f)(ctx, ex.key, ex.value)
					return nil
				})
			}
		}
	}
}

// ToMap returns all non-expired cache entries as a [*sync.Map]. Each key in the
// returned map is a string and each value has the cache's value type V. Several
// goroutines fill the map at once, one per range of shards (see [WithMaxWorkers]
// for how many). An expired entry met on the way is evicted, as in [Gache.Range].
// The operation can be cancelled via the provided context, which each goroutine
// checks once per 64 shards.
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
	g.loop(ctx, func(k string, v *value[string, V]) bool {
		if val, _, ok := v.readValue(); ok {
			m.Store(k, val)
		}
		return true
	})
	return m
}

// fanOut splits the slen shards into at most n = min(nprocs, slen) contiguous
// ranges (nprocs >= 1) and calls run(worker, start, end) once per non-empty range,
// with worker < n. Worker n-1 runs on the caller's goroutine; every other worker
// gets its own goroutine (when rounding leaves fewer than n ranges, all of them
// run on goroutines). It returns the sum of the run results after all ranges have
// finished.
func fanOut(nprocs int, run func(worker, start, end int) uint64) uint64 {
	nprocs, chunkSize := fanOutChunk(nprocs)
	// One allocation for both fields: they are shared with the goroutines.
	var s struct {
		wg    sync.WaitGroup
		total atomic.Uint64
	}
	var last uint64
	for i := range nprocs {
		start := i * chunkSize
		if start >= slen {
			break
		}
		end := min(start+chunkSize, slen)
		if i == nprocs-1 {
			last = run(i, start, end)
			break
		}
		s.wg.Go(func() {
			if n := run(i, start, end); n > 0 {
				s.total.Add(n)
			}
		})
	}
	s.wg.Wait()
	return s.total.Load() + last
}

// fanOutChunk returns n = min(nprocs, slen), the number of workers of
// fanOut(nprocs), and the number of shards in each of its ranges: worker i gets
// shards [i*chunkSize, min((i+1)*chunkSize, slen)).
func fanOutChunk(nprocs int) (n, chunkSize int) {
	n = min(nprocs, slen)
	return n, (slen + n - 1) / n
}

// gatherDirect extracts elements in parallel across shards. It sizes one array,
// buf, from the current shard lengths and gives each fanOut worker its own
// window of it, so chunks[w], worker w's elements, is a prefix of that window
// (see flatten). A worker that finds more elements than its window holds (keys
// inserted meanwhile) lets append move its chunk out of buf.
//
// One array made before the workers start, which flatten usually returns as is,
// in place of a chunk allocated by each worker and a second array to copy them
// into: half the bytes, and no proportional GC sweep charged to the workers'
// allocations or to that copy. BenchmarkGache_Values -58% time and -50% B/op,
// BenchmarkGache_Keys -58% and -51% (benchstat, interleaved A/B, 2026-11-09,
// mission gache-critical-path-slim).
func gatherDirect[V any, T any](
	ctx context.Context,
	g *gache[V],
	withValue bool,
	extract func(k string, val V) T,
) (chunks [][]T, buf []T) {
	nprocs, chunkSize := fanOutChunk(g.numWorkers())
	// ends[w] is the end of worker w's window in buf.
	ends := make([]int, nprocs)
	total := 0
	for w := range nprocs {
		for _, shard := range g.shards[min(w*chunkSize, slen):min((w+1)*chunkSize, slen)] {
			total += shard.length()
		}
		ends[w] = total
	}
	if total == 0 {
		return nil, nil
	}

	now := fastime.UnixNanoNow()
	cancelable := ctx.Done() != nil

	buf = make([]T, total)
	chunks = make([][]T, nprocs)
	for w, end := range ends {
		start := 0
		if w > 0 {
			start = ends[w-1]
		}
		chunks[w] = buf[start:start:end]
	}
	fanOut(nprocs, func(worker, start, end int) uint64 {
		chunk := chunks[worker]
		for j, shard := range g.shards[start:end] {
			if cancelable && j&63 == 0 && ctx.Err() != nil {
				break
			}
			if withValue {
				shard.rangeShardVal(now, func(k string, val V, _ int64) {
					chunk = append(chunk, extract(k, val))
				})
			} else {
				shard.rangeShard(func(k string, v *value[string, V]) {
					if exp := v.exp().Load(); exp > 0 && now > exp {
						return
					}
					var zero V
					chunk = append(chunk, extract(k, zero))
				})
			}
		}
		chunks[worker] = chunk
		return 0
	})
	return chunks, buf
}

// flatten concatenates the chunks gatherDirect returned with buf into one
// never-nil slice. While every chunk is still a prefix of its window in buf, the
// chunks are moved down to the front of buf, which is returned: chunk w starts at
// or after the end of the chunks before it, and ends before window w+1, so no
// copy overwrites a chunk not yet moved. If append has moved a chunk out of buf,
// the result is a fresh slice.
func flatten[T any](chunks [][]T, buf []T) []T {
	// The windows cover buf and a chunk in its window has the window's capacity;
	// append moves a chunk only to a larger one, so the capacities add up to
	// len(buf) exactly when no chunk has moved.
	total, caps := 0, 0
	for _, chunk := range chunks {
		total += len(chunk)
		caps += cap(chunk)
	}
	out := buf
	if caps != len(buf) {
		out = make([]T, total)
	}
	pos := 0
	for _, chunk := range chunks {
		// Full windows are already in place: only a chunk after a short one moves.
		if len(chunk) != 0 && &out[pos] != &chunk[0] {
			copy(out[pos:], chunk)
		}
		pos += len(chunk)
	}
	if out == nil {
		return make([]T, 0)
	}
	clear(out[pos:]) // the tail no element was gathered into, after missed entries
	return out[:pos]
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
	chunks, _ := gatherDirect(ctx, g, true, func(k string, val V) kv[V] {
		return kv[V]{key: k, value: val}
	})
	if len(chunks) == 0 {
		return make(map[string]V, 0)
	}

	var totalActual int
	for _, chunk := range chunks {
		totalActual += len(chunk)
	}
	m = make(map[string]V, totalActual)
	for _, chunk := range chunks {
		for _, item := range chunk {
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
func (g *gache[V]) Keys(ctx context.Context) []string {
	return flatten(gatherDirect(ctx, g, false, func(k string, _ V) string {
		return k
	}))
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
func (g *gache[V]) Values(ctx context.Context) []V {
	return flatten(gatherDirect(ctx, g, true, func(_ string, val V) V {
		return val
	}))
}

// get returns value & exists from key.
func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	shard, h := g.shard(key)
	v, expire, ok = shard.loadValue(h, key)
	if !ok {
		return v, 0, false
	}

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
	shard, h := g.shard(key)
	shard.store(h, key, val, expire)
}

// SetWithExpire stores the key-value pair with the given expiration duration.
// If expire is zero or negative (such as [NoTTL]), the entry will never expire
// automatically.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("session", "sid_xyz", 30*time.Minute)
func (g *gache[V]) SetWithExpire(key string, val V, expire time.Duration) {
	g.set(key, val, int64(expire))
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
	shard, h := g.shard(key)
	return shard.loadAndDelete(h, key)
}

// expiration evicts key after a caller saw its entry expired. The entry is
// re-checked under its lock: a Set that replaced the expired value in the meantime
// is kept and not reported to the expired hook. The report never waits for the
// hook queue (offerExpired).
func (g *gache[V]) expiration(key string) {
	shard, h := g.shard(key)
	if v, loaded := shard.expireIf(h, key, fastime.UnixNanoNow()); loaded && g.expFuncEnabled.Load() {
		g.offerExpired(key, v)
	}
}

// offerExpired queues an expired entry for the hook daemon without waiting: when
// the queue is full the notification is dropped (the entry is evicted already).
// The read paths use it, since nothing drains the queue while no daemon runs, and
// the daemon's hooks that call them would wait for themselves. Callers check
// expFuncEnabled first.
func (g *gache[V]) offerExpired(key string, v V) {
	select {
	case g.initExpChan() <- kv[V]{key: key, value: v}:
	default:
	}
}

// sendExpired queues an expired entry for the hook daemon, for DeleteExpired. When
// the queue is full it waits for room, or gives up once ctx is done (an entry that
// fits is always queued, even for a done ctx). Callers check expFuncEnabled first.
func (g *gache[V]) sendExpired(ctx context.Context, key string, v V) {
	ch, ev := g.initExpChan(), kv[V]{key: key, value: v}
	select {
	case ch <- ev:
	default:
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
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
// With the expired hook enabled, each removed entry is queued for the hook
// daemon. When the queue is full DeleteExpired waits for room, which only a
// running daemon provides, until ctx is done: the entries that do not fit are then
// dropped without a hook call (they are deleted all the same). So with the hook
// enabled and no daemon running, a ctx that is never done blocks DeleteExpired
// once the queue is full.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("short", "v", time.Millisecond)
//	time.Sleep(5 * time.Millisecond)
//
//	n := gc.DeleteExpired(context.Background())
//	fmt.Printf("removed %d expired entries\n", n)
func (g *gache[V]) DeleteExpired(ctx context.Context) uint64 {
	now := fastime.UnixNanoNow()
	cancelable := ctx.Done() != nil

	var onExpired func(string, V)
	if g.expFuncEnabled.Load() {
		onExpired = func(k string, v V) { g.sendExpired(ctx, k, v) }
	}

	return fanOut(g.numWorkers(), func(_, start, end int) (expired uint64) {
		for j := start; j < end; j++ {
			if cancelable && (j-start)&63 == 0 && ctx.Err() != nil {
				break
			}
			expired += g.shards[j].deleteExpired(now, onExpired)
		}
		return expired
	})
}

// Range iterates over every non-expired entry in the cache, calling f for each
// one. The function f receives the key, value, and expiration timestamp (unix
// nanoseconds). It returns the receiver for chaining.
//
// Several goroutines walk the cache at once, one per range of shards (see
// [WithMaxWorkers] for how many), so f is called from several goroutines
// concurrently and must be safe for that; the order is arbitrary. When f returns
// false only the goroutine that called it stops: the others carry on, and f can be
// called again after it returned false. Use WithMaxWorkers(1) for a sequential
// walk that returning false stops entirely. Cancelling the context stops each
// goroutine at its next check, which it makes once per 64 shards. An expired
// entry met on the way is evicted, and queued for the expired hook (see
// [Gache.EnableExpiredHook]), instead of being passed to f.
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
	g.loop(ctx, func(k string, v *value[string, V]) bool {
		val, exp, ok := v.readValue()
		if !ok {
			return true
		}
		return f(k, val, exp)
	})
	return g
}

// numWorkers returns the number of workers of a fan-out: runtime.GOMAXPROCS(0),
// capped by maxWorkers when that is positive.
func (g *gache[V]) numWorkers() int {
	nprocs := runtime.GOMAXPROCS(0)
	if g.maxWorkers > 0 {
		nprocs = min(nprocs, g.maxWorkers)
	}
	return nprocs
}

// hookWorkers returns how many expired hooks may run at once: twice numWorkers,
// capped by maxWorkers when that is positive. It is at least 1.
func (g *gache[V]) hookWorkers() int {
	n := g.numWorkers() * 2
	if g.maxWorkers > 0 {
		n = min(n, g.maxWorkers)
	}
	return n
}

// loop calls f for every live node, shard range by shard range in parallel;
// expired nodes are deleted instead (see expiration). A worker whose f returns
// false stops; the others carry on. It walks the chains itself: going through
// rangeShard adds a closure call per node, +2.3% on BenchmarkGache_Loop/10000
// (benchstat, interleaved A/B, 2026-11-09, mission gache-critical-path-slim).
func (g *gache[V]) loop(ctx context.Context, f func(string, *value[string, V]) bool) {
	now := fastime.UnixNanoNow()
	cancelable := ctx.Done() != nil

	fanOut(g.numWorkers(), func(_, start, end int) uint64 {
		for j, shard := range g.shards[start:end] {
			if cancelable && j&63 == 0 && ctx.Err() != nil {
				return 0
			}
			for _, head := range shard.readSnapshot().m {
				for n := head; n != nil; n = loadNext(n) {
					if !n.live() {
						continue
					}
					if exp := n.exp().Load(); exp > 0 && now > exp {
						g.expiration(n.key)
						continue
					}
					if !f(n.key, n) {
						return 0
					}
				}
			}
		}
		return 0
	})
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
		l += g.shards[i].length()
	}
	return l
}

// Size returns an estimate of the in-memory size of the cache in bytes. The
// returned value includes the gache struct itself (most of it the array of shard
// pointers) as well as the size reported by each internal shard: its maps and its nodes, a node
// holding the key string header and the value V as stored. It excludes the bytes
// the key strings and the heap data of the values point to, and the buffer of the
// expired hook queue. Every heap object is charged as the Go allocator rounds it
// (size class, malloc header, pages); it is still an estimate. Size takes each
// shard lock briefly, one shard at a time, and allocates nothing.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.Set("k", "v")
//	fmt.Printf("cache size: %d bytes\n", gc.Size())
func (g *gache[V]) Size() (size uintptr) {
	size = allocSize(unsafe.Sizeof(*g), true)
	for _, shard := range g.shards {
		size += shard.size()
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
			g.shards[i].initReserve(sizePerShard)
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

// Stop cancels the background expiration daemon started by [Gache.StartExpired],
// including one started a moment ago. The daemon stops sweeping, stops serving the
// hook queue and a hook that is already running sees its context cancelled; the
// daemon winds down asynchronously and Stop does not wait for it (an entry it takes
// off the queue before it notices the cancellation is served with the cancelled
// context). Entries still queued stay queued, and the next [Gache.StartExpired]
// serves them. Stop is a no-op if no daemon was started.
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
		g.shards[i].clear()
	}
}

// ExtendExpire extends the expiration of an existing non-expired entry by
// addExp. If the key does not exist, ExtendExpire is a no-op. An entry that has
// already expired is not extended but evicted, and queued for the expired hook if
// it is enabled (see [Gache.EnableExpiredHook]). ExtendExpire is also a no-op for
// an entry without a TTL (see [NoTTL]), which stays without one.
//
// Example:
//
//	gc := gache.New[string]()
//	gc.SetWithExpire("sess", "data", 10*time.Minute)
//
//	// User activity detected — extend the session by another 10 minutes.
//	gc.ExtendExpire("sess", 10*time.Minute)
func (g *gache[V]) ExtendExpire(key string, addExp time.Duration) {
	shard, h := g.shard(key)
	n, ok := shard.loadNode(h, key)
	if !ok {
		return
	}
	// The lock, live and expiry check is repeated in GetRefreshWithDur and
	// SetWithExpireIfNotExists on purpose: a shared value method adds a call frame
	// (sync.Mutex.Lock alone costs 68 of the 80 inline budget, go1.27.3
	// -gcflags=-m=2 on package sync, so the helper cannot inline), which costs
	// +14% on BenchmarkGache_ExtendExpire_HotKey (n=6, p=0.002, reproduced;
	// benchstat, interleaved A/B, 2026-11-09, mission gache-critical-path-slim)
	// whether it takes a closure or an op code.
	n.mu.Lock()
	if !n.live() {
		n.mu.Unlock()
		return
	}
	expire := n.exp().Load()
	if expire <= 0 {
		n.mu.Unlock()
		return
	}
	if fastime.UnixNanoNow() > expire {
		n.mu.Unlock()
		g.expiration(key)
		return
	}
	n.exp().Store(expire + int64(addExp))
	n.mu.Unlock()
}

// GetRefresh retrieves the value for key and, if the entry exists and has not
// expired, resets its expiration to the cache's default TTL. This is equivalent
// to calling GetRefreshWithDur with the default expiration duration. An entry
// that has already expired is evicted, and queued for the expired hook if it is
// enabled, and GetRefresh reports false.
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
// implementing sliding-window TTL patterns. As with [Gache.SetWithExpire], a d
// of zero or less (such as [NoTTL]) leaves the entry without an expiration. An
// entry that has already expired is not refreshed but evicted, and queued for the
// expired hook if it is enabled (see [Gache.EnableExpiredHook]), and
// GetRefreshWithDur reports false.
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
	shard, h := g.shard(key)
	n, found := shard.loadNode(h, key)
	if !found {
		return v, false
	}
	n.mu.Lock()
	if !n.live() {
		n.mu.Unlock()
		return v, false
	}
	expire := n.exp().Load()
	if expire > 0 && fastime.UnixNanoNow() > expire {
		n.mu.Unlock()
		g.expiration(key)
		return v, false
	}
	exp := int64(d)
	if exp > 0 {
		exp += fastime.UnixNanoNow()
	}
	n.exp().Store(exp)
	v = n.readValRaw()
	n.mu.Unlock()
	return v, true
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
	shard, h := g.shard(key)
	n, found := shard.loadNode(h, key)
	if !found {
		return v, false
	}
	v, _, ok = n.readValue()
	return v, ok
}

// Pop atomically retrieves and removes the entry for key. It returns the value
// and true if the key existed and had not expired; otherwise it returns the
// zero value and false. An entry that had expired is removed all the same, and
// queued for the expired hook if it is enabled (see [Gache.EnableExpiredHook]).
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
	shard, h := g.shard(key)
	v, expire, loaded := shard.popIf(h, key, nil)
	if !loaded {
		return v, false
	}
	if expire <= 0 || fastime.UnixNanoNow() <= expire {
		return v, true
	}
	if g.expFuncEnabled.Load() {
		g.offerExpired(key, v)
	}
	return v, false
}

// SetIfNotExists stores the key-value pair only if key is not already present
// (or has expired) in the cache. The default expiration is used. This provides
// a simple compare-and-set semantic for cache population. Replacing an expired
// entry does not call the expired hook.
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
// cache. As with [Gache.SetWithExpire], a d of zero or less stores the entry
// without an expiration. Replacing an expired entry does not call the expired
// hook.
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

	shard, h := g.shard(key)
	for {
		n, found := shard.loadNode(h, key)
		if !found {
			// No live node; loadOrStore atomically installs (or revives a
			// tombstone). If it observes a live node concurrently created, it
			// returns loaded=true and we fall through to the expiry check.
			if _, loaded := shard.loadOrStore(h, key, val, exp); !loaded {
				return
			}
			continue
		}

		// Lock-free fast path: if the node is live and not expired, do not overwrite.
		if n.live() {
			expire := n.exp().Load()
			if expire <= 0 || fastime.UnixNanoNow() <= expire {
				return
			}
		}

		n.mu.Lock()
		if !n.live() {
			n.mu.Unlock()
			continue
		}
		expire := n.exp().Load()
		if expire <= 0 || fastime.UnixNanoNow() <= expire {
			// Live and valid: do not overwrite.
			n.mu.Unlock()
			return
		}
		// Expired: replace in place.
		n.writeVal(val)
		n.exp().Store(exp)
		n.mu.Unlock()
		return
	}
}
