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
	"golang.org/x/sync/errgroup"
)

type (
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

	gache[V any] struct {
		shards         [slen]*Map[string, value[V]]
		cancel         atomic.Pointer[context.CancelFunc]
		expFunc        func(context.Context, string, V)
		expFuncEnabled bool
		expire         int64
		maxKeyLength   uint64
		maxWorkers     int

		clock       Clock
		tw          *timingWheel
		lifecycleMu sync.Mutex
	}

	value[V any] struct {
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
	slen                = 8192
	mask                = 0x1FFF
	NoTTL time.Duration = -1
)

var hashSeed = maphash.MakeSeed()

func New[V any](opts ...Option[V]) Gache[V] {
	g := new(gache[V])
	g.clock.now.Store(time.Now().UnixNano())
	g.tw = newTimingWheel(14, 10*time.Millisecond, g.clock.Now())

	for i := range g.shards {
		g.shards[i] = newMap[string, value[V]]()
	}
	for _, opt := range append([]Option[V]{
		WithDefaultExpiration[V](30 * time.Second),
		WithMaxKeyLength[V](256),
		WithMaxWorkers[V](runtime.NumCPU() * 2),
	}, opts...) {
		opt(g)
	}
	return g
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

func (v *value[V]) isValid(key string, now int64) (valid bool, match bool) {
	if v.key != key {
		return false, false
	}
	return v.expire <= 0 || now <= v.expire, true
}

func (g *gache[V]) SetDefaultExpire(ex time.Duration) Gache[V] {
	atomic.StoreInt64(&g.expire, *(*int64)(unsafe.Pointer(&ex)))
	return g
}

func (g *gache[V]) EnableExpiredHook() Gache[V] {
	g.expFuncEnabled = true
	return g
}

func (g *gache[V]) DisableExpiredHook() Gache[V] {
	g.expFuncEnabled = false
	return g
}

func (g *gache[V]) SetExpiredHook(f func(context.Context, string, V)) Gache[V] {
	g.expFunc = f
	return g
}

func (g *gache[V]) StartExpired(ctx context.Context, dur time.Duration) Gache[V] {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()

	if g.cancel.Load() != nil {
		return g
	}

	ctx, cancel := context.WithCancel(ctx)
	g.cancel.Store(&cancel)

	g.clock.Start(ctx, 10*time.Millisecond)

	go func() {
		tick := time.NewTicker(dur)
		defer tick.Stop()

		eg, egctx := errgroup.WithContext(ctx)
		nprocs := g.numWorkers()
		eg.SetLimit(min(nprocs*2, g.maxWorkers))

		for {
			select {
			case <-egctx.Done():
				eg.Wait()
				return
			case <-tick.C:
				now := g.clock.Now()
				expiredItems := g.tw.advance(now)
				if len(expiredItems) > 0 {
					for _, item := range expiredItems {
						key := item.key
						exp := item.expire

						// Verify expiration
						shard := g.shards[getShardID(key, g.maxKeyLength)]
						val, ok := shard.Load(key)
						if ok && val.expire == exp && (val.expire <= 0 || now > val.expire) {
							shard.Delete(key)
							if g.expFuncEnabled && g.expFunc != nil {
								// Execute hook asynchronously
								eg.Go(func() error {
									g.expFunc(egctx, key, val.val)
									return nil
								})
							}
						}
					}
				}
			}
		}
	}()
	return g
}

func (g *gache[V]) ToMap(ctx context.Context) (m *sync.Map) {
	m = new(sync.Map)
	_ = g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		if v.key == k {
			m.Store(k, v.val)
		}
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
		if v.key == k {
			item := extract(k, v)
			chunks[workerID] = append(chunks[workerID], item)
		}
		return true
	})

	return chunks, totalLen
}

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

func (g *gache[V]) get(key string) (v V, expire int64, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].Load(key)
	if !ok {
		return v, 0, false
	}

	if val.key != key {
		return v, 0, false
	}

	now := g.clock.Now()
	if val.expire <= 0 || now <= val.expire {
		return val.val, val.expire, true
	}

	g.expiration(key, val)
	return val.val, val.expire, false
}

func (g *gache[V]) Get(key string) (v V, ok bool) {
	v, _, ok = g.get(key)
	return v, ok
}

func (g *gache[V]) GetWithExpire(key string) (v V, expire int64, ok bool) {
	return g.get(key)
}

func (g *gache[V]) set(key string, val V, expire int64) {
	if expire > 0 {
		expire = g.clock.Now() + expire
	}
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	newVal := value[V]{
		key:    key,
		val:    val,
		expire: expire,
	}
	shard.Store(key, newVal)

	if expire > 0 {
		g.tw.add(key, expire)
	}
}

func (g *gache[V]) SetWithExpire(key string, val V, expire time.Duration) {
	g.set(key, val, *(*int64)(unsafe.Pointer(&expire)))
}

func (g *gache[V]) Set(key string, val V) {
	g.set(key, val, atomic.LoadInt64(&g.expire))
}

func (g *gache[V]) Delete(key string) (v V, loaded bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded := shard.LoadAndDelete(key)
	if loaded {
		if val.key != key {
			return v, false
		}
		return val.val, true
	}
	return v, false
}

func (g *gache[V]) expiration(key string, v value[V]) {
	_, loaded := g.Delete(key)
	if loaded && g.expFuncEnabled && g.expFunc != nil {
		// Run in a new goroutine to not block
		go g.expFunc(context.Background(), key, v.val)
	}
}

func (g *gache[V]) DeleteExpired(ctx context.Context) (expired uint64) {
	return g.loop(ctx, func(workerID int, k string, v *value[V]) bool {
		return true
	})
}

func (g *gache[V]) Range(ctx context.Context, f func(string, V, int64) bool) Gache[V] {
	// Dynamically collect active keys to avoid long map locks
	keys := g.Keys(ctx)
	for _, k := range keys {
		if ctx.Err() != nil {
			break
		}
		v, expire, ok := g.GetWithExpire(k)
		if ok {
			if !f(k, v, expire) {
				break
			}
		}
	}
	return g
}

func (g *gache[V]) numWorkers() int {
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
	now := g.clock.Now()

	var fn func(workerID int, k string, v *value[V]) bool
	if f != nil {
		fn = func(workerID int, k string, v *value[V]) bool {
			if v != nil {
				valid, match := v.isValid(k, now)
				if !match {
					return true
				}
				if !valid {
					g.expiration(k, *v)
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
				valid, match := v.isValid(k, now)
				if match && !valid {
					g.expiration(k, *v)
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
			g.shards[i].Range(func(k string, v value[V]) bool { return fn(0, k, &v) })
		}
		return atomic.LoadUint64(&expiredRows)
	}

	var idx atomic.Uint64
	var wg sync.WaitGroup
	worker := func(workerID int) {
		defer wg.Done()
		for {
			endIdx := idx.Add(16)
			startIdx := endIdx - 16
			if startIdx >= slen || ctx.Err() != nil {
				return
			}
			if endIdx > slen {
				endIdx = slen
			}
			for j := startIdx; j < endIdx; j++ {
				g.shards[j].Range(func(k string, v value[V]) bool { return fn(workerID, k, &v) })
			}
		}
	}

	wg.Add(nprocs)
	for i := range nprocs {
		go worker(i)
	}
	wg.Wait()
	return atomic.LoadUint64(&expiredRows)
}

func (g *gache[V]) Len() (l int) {
	for i := range g.shards {
		l += g.shards[i].Len()
	}
	return l
}

func (g *gache[V]) Size() (size uintptr) {
	size += unsafe.Sizeof(g.expFuncEnabled)
	size += unsafe.Sizeof(g.expire)
	size += unsafe.Sizeof(g.cancel)
	size += unsafe.Sizeof(g.expFunc)
	for _, shard := range g.shards {
		size += shard.Size()
	}
	return size
}

func (g *gache[V]) Write(ctx context.Context, w io.Writer) error {
	m := g.ToRawMap(ctx)
	gob.Register(map[string]V{})
	return gob.NewEncoder(w).Encode(&m)
}

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

func (g *gache[V]) Stop() {
	g.lifecycleMu.Lock()
	defer g.lifecycleMu.Unlock()

	if c := g.cancel.Load(); c != nil {
		cancel := *c
		cancel()
		g.cancel.Store(nil)
	}
}

func (g *gache[V]) Clear() {
	for i := range g.shards {
		if g.shards[i] == nil {
			g.shards[i] = newMap[string, value[V]]()
		} else {
			g.shards[i].Clear()
		}
	}
}

func (g *gache[V]) ExtendExpire(key string, addExp time.Duration) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]

	// Fast path check
	val, ok := shard.Load(key)
	if !ok {
		return
	}
	now := g.clock.Now()
	valid, match := val.isValid(key, now)
	if !match || !valid {
		if !valid {
			g.expiration(key, val)
		}
		return
	}

	// CAS update
	for {
		newVal := val
		newVal.expire = val.expire + int64(addExp)
		if shard.CompareAndSwap(key, val, newVal) {
			if newVal.expire > 0 {
				g.tw.add(key, newVal.expire)
			}
			return
		}
		// retry
		val, ok = shard.Load(key)
		if !ok {
			return
		}
		valid, match = val.isValid(key, now)
		if !match || !valid {
			if !valid {
				g.expiration(key, val)
			}
			return
		}
	}
}

func (g *gache[V]) GetRefresh(key string) (V, bool) {
	return g.GetRefreshWithDur(key, time.Duration(atomic.LoadInt64(&g.expire)))
}

func (g *gache[V]) GetRefreshWithDur(key string, d time.Duration) (v V, ok bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]

	val, ok := shard.Load(key)
	if !ok {
		return v, false
	}
	now := g.clock.Now()
	valid, match := val.isValid(key, now)
	if !match || !valid {
		if !valid {
			g.expiration(key, val)
		}
		return v, false
	}

	for {
		newVal := val
		newVal.expire = now + int64(d)
		if shard.CompareAndSwap(key, val, newVal) {
			if newVal.expire > 0 {
				g.tw.add(key, newVal.expire)
			}
			return newVal.val, true
		}
		// retry
		val, ok = shard.Load(key)
		if !ok {
			return v, false
		}
		valid, match = val.isValid(key, now)
		if !match || !valid {
			if !valid {
				g.expiration(key, val)
			}
			return v, false
		}
	}
}

func (g *gache[V]) GetWithIgnoredExpire(key string) (v V, ok bool) {
	val, ok := g.shards[getShardID(key, g.maxKeyLength)].Load(key)
	if !ok {
		return v, false
	}
	if val.key != key {
		return v, false
	}
	return val.val, true
}

func (g *gache[V]) Pop(key string) (v V, ok bool) {
	shard := g.shards[getShardID(key, g.maxKeyLength)]
	val, loaded := shard.LoadAndDelete(key)
	if !loaded {
		return v, false
	}
	if val.key != key {
		return v, false
	}

	now := g.clock.Now()
	valid := val.expire <= 0 || now <= val.expire

	if valid {
		return val.val, true
	}
	if g.expFuncEnabled && g.expFunc != nil {
		go g.expFunc(context.Background(), key, val.val)
	}
	return v, false
}

func (g *gache[V]) SetIfNotExists(key string, val V) {
	g.SetWithExpireIfNotExists(key, val, time.Duration(atomic.LoadInt64(&g.expire)))
}

func (g *gache[V]) SetWithExpireIfNotExists(key string, val V, d time.Duration) {
	exp := int64(d)
	now := g.clock.Now()
	if exp > 0 {
		exp += now
	}

	newVal := value[V]{
		key:    key,
		val:    val,
		expire: exp,
	}

	shard := g.shards[getShardID(key, g.maxKeyLength)]

	for {
		actual, loaded := shard.LoadOrStore(key, newVal)
		if !loaded {
			if exp > 0 {
				g.tw.add(key, exp)
			}
			return
		}

		valid, match := actual.isValid(key, now)
		if !match {
			continue
		}
		if valid {
			return
		}

		if shard.CompareAndSwap(key, actual, newVal) {
			if exp > 0 {
				g.tw.add(key, exp)
			}
			return
		}
	}
}
