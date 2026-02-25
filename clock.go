package gache

import (
	"sync/atomic"
	"time"
)

var (
	now int64
)

func init() {
	updateTime()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			updateTime()
		}
	}()
}

func updateTime() {
	atomic.StoreInt64(&now, time.Now().UnixNano())
}

// Now returns the current unix nano timestamp from the cached atomic value.
func Now() int64 {
	return atomic.LoadInt64(&now)
}
