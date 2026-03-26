package gache

import (
	"context"
	"sync/atomic"
	"time"
)

type Clock struct {
	now atomic.Int64
}

func (c *Clock) Start(ctx context.Context, interval time.Duration) {
	c.now.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				c.now.Store(t.UnixNano())
			}
		}
	}()
}

func (c *Clock) Now() int64 {
	return c.now.Load()
}
