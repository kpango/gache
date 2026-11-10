package gache

import (
	"context"
	"time"
)

type Option[V any] func(g *gache[V]) error

// WithDefaultExpirationString is [WithDefaultExpiration] with the duration given as
// a string that [time.ParseDuration] accepts, such as "5m". An empty string leaves
// the default TTL (30 seconds) unchanged, and a duration of zero or less ("0s",
// "-1s") disables expiry. A string that does not parse makes the option return an
// error, which [New] ignores: the default TTL then stays.
func WithDefaultExpirationString[V any](t string) Option[V] {
	return func(g *gache[V]) error {
		if len(t) != 0 {
			dur, err := time.ParseDuration(t)
			if err != nil {
				return err
			}
			return WithDefaultExpiration[V](dur)(g)
		}
		return nil
	}
}

// WithDefaultExpiration sets the TTL that [Gache.Set], [Gache.SetIfNotExists] and
// [Gache.GetRefresh] use, as [Gache.SetDefaultExpire] does after construction. A
// dur of zero or less, such as [NoTTL], disables expiry: the entries stored with
// the default never expire. Without this option the default TTL is 30 seconds.
func WithDefaultExpiration[V any](dur time.Duration) Option[V] {
	return func(g *gache[V]) error {
		g.expire = dur.Nanoseconds()
		return nil
	}
}

// WithExpiredHookFunc registers f as the expired-entry hook and enables it, as
// [Gache.SetExpiredHook] followed by [Gache.EnableExpiredHook] do. The daemon
// started by [Gache.StartExpired] calls f, from several goroutines at once, for
// each entry queued for it. A nil f is ignored.
func WithExpiredHookFunc[V any](f func(ctx context.Context, key string, v V)) Option[V] {
	return func(g *gache[V]) error {
		if f != nil {
			g.expFunc.Store(&f)
			g.expFuncEnabled.Store(true)
			g.initExpChan()
		}
		return nil
	}
}

// WithMaxKeyLength sets the maximum number of bytes used from each key when
// computing the shard ID: a key longer than kl is hashed from its first kl bytes
// only, so keys that share that prefix map to the same hash. Distinct keys stay
// distinct entries; a collision only makes them share a shard and a bucket chain.
// A value of 0 means no prefix limit, not that the full key is hashed: a key is
// then hashed as follows, whatever its length. The default is 256 bytes.
//
//   - A key of 0 bytes maps to 0 and a key of 1 byte to its byte value, without
//     hashing.
//   - A key of 2 through 32 bytes is hashed with maphash.
//   - A key of 33 through 64 bytes is hashed with xxh3 under a seed drawn once per
//     process, or with maphash on arm64.
//   - A key longer than 64 bytes is hashed from a 64-byte sample of four 16-byte
//     windows (the first 16 bytes, the 16 bytes at a quarter and at three quarters
//     of the length, and the last 16 bytes) with the length mixed in, using the
//     same xxh3 seed, or maphash on arm64. Keys of the same length that differ
//     only outside the windows collide.
func WithMaxKeyLength[V any](kl uint64) Option[V] {
	return func(g *gache[V]) error {
		g.maxKeyLength = kl
		return nil
	}
}

// WithMaxWorkers sets workers, the limit on the goroutines of the operations that
// fan work out across shards and on the expired hooks the daemon runs at once.
//
//   - Fan-out, used by Range, DeleteExpired, ToMap, Keys, Values, ToRawMap, Write
//     and Read: an operation runs on at most min(GOMAXPROCS, workers) goroutines,
//     or on at most GOMAXPROCS when workers <= 0.
//   - Hooks: the daemon started by [Gache.StartExpired] runs at most
//     min(2*GOMAXPROCS, workers) expired hooks at once, or 2*GOMAXPROCS when
//     workers <= 0.
//
// GOMAXPROCS (runtime.GOMAXPROCS(0)) is read when the operation, or the daemon,
// starts. Without this option workers is 2*runtime.NumCPU(), read by [New].
func WithMaxWorkers[V any](workers int) Option[V] {
	return func(g *gache[V]) error {
		g.maxWorkers = workers
		return nil
	}
}
