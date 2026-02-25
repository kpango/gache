package gache

import (
	"context"
	"time"
)

type Option[V any] func(g *gache[V]) error

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

func WithDefaultExpiration[V any](dur time.Duration) Option[V] {
	return func(g *gache[V]) error {
		if dur > 0 {
			g.expire = dur.Nanoseconds()
		}
		return nil
	}
}

func WithExpiredHookFunc[V any](f func(ctx context.Context, key string, v V)) Option[V] {
	return func(g *gache[V]) error {
		if f != nil {
			g.expFunc = f
			g.expFuncEnabled = true
		}
		return nil
	}
}

// WithMaxKeyLength sets the maximum number of bytes used from each key when
// computing the shard ID. One-byte keys use a fast, non-hashing path; keys of
// length 2 through 32 bytes use maphash for hashing; longer keys use xxh3. A
// value of 0 means the full key is always used. The default is 256 bytes.
func WithMaxKeyLength[V any](kl uint64) Option[V] {
	return func(g *gache[V]) error {
		g.maxKeyLength = kl
		return nil
	}
}

// WithClockInterval sets the internal clock update interval and timing wheel resolution.
// Default is 100ms. Lower values provide more precise expiration but higher CPU usage.
func WithClockInterval[V any](interval time.Duration) Option[V] {
	return func(g *gache[V]) error {
		if interval > 0 {
			g.clockInterval = interval
		}
		return nil
	}
}

// WithTimingWheelBits sets the size of the timing wheel as a power of 2.
// Default is 14 (16384 buckets). Higher values allow covering longer expiration periods
// without bucket reuse collision issues, though collision impact is minimal.
func WithTimingWheelBits[V any](bits int) Option[V] {
	return func(g *gache[V]) error {
		if bits > 0 {
			g.wheelBits = bits
		}
		return nil
	}
}
