package gache

import (
	"sync"
	"sync/atomic"
	"time"
)

// Clock provides an efficient way to get the current time with reduced syscall overhead.
type Clock struct {
	now    atomic.Int64
	cancel chan struct{}
	once   sync.Once
}

// NewClock creates a new Clock that updates its time every interval.
func NewClock(interval time.Duration) *Clock {
	c := &Clock{
		cancel: make(chan struct{}),
	}
	c.update()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-c.cancel:
				return
			case <-ticker.C:
				c.update()
			}
		}
	}()
	return c
}

func (c *Clock) update() {
	c.now.Store(time.Now().UnixNano())
}

// Now returns the current cached unix nano timestamp.
func (c *Clock) Now() int64 {
	return c.now.Load()
}

// Stop stops the clock's background ticker.
func (c *Clock) Stop() {
	c.once.Do(func() {
		close(c.cancel)
	})
}
