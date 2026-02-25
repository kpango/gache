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

func WithMaxKeyLength[V any](kl uint64) Option[V] {
	return func(g *gache[V]) error {
		g.maxKeyLength = kl
		return nil
	}
}
