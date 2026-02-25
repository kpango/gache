package gache

import (
	"sync"
	"time"
)

const (
	// wheelBits is the power of 2 for the wheel size.
	// 14 bits = 16384 buckets.
	// At 100ms tick, span is ~27 minutes.
	wheelBits = 14
	wheelSize = 1 << wheelBits
	wheelMask = wheelSize - 1

	// tickDuration is the resolution of the timing wheel.
	// It should match the clock update frequency.
	tickDuration = int64(100 * time.Millisecond)
)

type timingWheel struct {
	mu           sync.Mutex
	buckets      [wheelSize][]string
	lastCheck    int64
}

// newTimingWheel creates a new timing wheel initialized with the current time.
func newTimingWheel(now int64) *timingWheel {
	return &timingWheel{
		lastCheck: now,
	}
}

// add inserts a key into the timing wheel based on its expiration timestamp.
func (tw *timingWheel) add(key string, expire int64) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Calculate bucket index.
	// We use integer division by tick duration masked by wheel size.
	idx := (expire / tickDuration) & wheelMask
	tw.buckets[idx] = append(tw.buckets[idx], key)
}

// advance moves the wheel forward to 'now' and returns expired keys.
func (tw *timingWheel) advance(now int64) []string {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if now <= tw.lastCheck {
		return nil
	}

	startTick := tw.lastCheck / tickDuration
	endTick := now / tickDuration

	if startTick == endTick {
		tw.lastCheck = now
		return nil
	}

	var expiredKeys []string

	count := endTick - startTick
	if count > wheelSize {
		count = wheelSize
	}

	for i := int64(1); i <= count; i++ {
		currentTick := startTick + i
		idx := currentTick & wheelMask

		bucket := tw.buckets[idx]
		if len(bucket) > 0 {
			expiredKeys = append(expiredKeys, bucket...)
			// Reuse capacity to avoid allocation next time
			tw.buckets[idx] = bucket[:0]
		}
	}

	tw.lastCheck = now
	return expiredKeys
}
