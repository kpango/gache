package gache

import (
	"context"
	"time"
)

type Option func(g *gache) error

var (
	defaultOpts = []Option{
		WithDefaultExpiration(time.Second * 30),
	}
)

func WithDefaultExpirationString(t string) Option {
	return func(g *gache) error {
		if len(t) != 0 {
			dur, err := time.ParseDuration(t)
			if err != nil {
				return err
			}
			return WithDefaultExpiration(dur)(g)
		}
		return nil
	}
}

func WithDefaultExpiration(dur time.Duration) Option {
	return func(g *gache) error {
		if dur > 0 {
			g.expire = dur.Nanoseconds()
		}
		return nil
	}
}

func WithExpiredHookFunc(f func(ctx context.Context, key string)) Option {
	return func(g *gache) error {
		if f != nil {
			g.expFunc = f
			g.expFuncEnabled = true
		}
		return nil
	}
}
