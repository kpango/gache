package gache

import (
	"sync"
	"time"
)

type timingWheel struct {
	mu           sync.Mutex
	buckets      [][]string
	lastCheck    int64
	tickDuration int64
	wheelSize    int64
	wheelMask    int64
}

// newTimingWheel creates a new timing wheel initialized with the current time.
func newTimingWheel(now int64, tickDuration time.Duration, wheelBits int) *timingWheel {
	if wheelBits <= 0 {
		wheelBits = defaultWheelBits
	}
	wheelSize := int64(1 << wheelBits)
	return &timingWheel{
		lastCheck:    now,
		tickDuration: int64(tickDuration),
		wheelSize:    wheelSize,
		wheelMask:    wheelSize - 1,
		buckets:      make([][]string, wheelSize),
	}
}

// add inserts a key into the timing wheel based on its expiration timestamp.
func (tw *timingWheel) add(key string, expire int64) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	// Calculate bucket index.
	// We use integer division by tick duration masked by wheel size.
	idx := (expire / tw.tickDuration) & tw.wheelMask
	tw.buckets[idx] = append(tw.buckets[idx], key)
}

// advance moves the wheel forward to 'now' and returns expired keys.
func (tw *timingWheel) advance(now int64) []string {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if now <= tw.lastCheck {
		return nil
	}

	startTick := tw.lastCheck / tw.tickDuration
	endTick := now / tw.tickDuration

	if startTick == endTick {
		tw.lastCheck = now
		return nil
	}

	var expiredKeys []string

	count := endTick - startTick
	if count > tw.wheelSize {
		count = tw.wheelSize
	}

	for i := int64(1); i <= count; i++ {
		currentTick := startTick + i
		idx := currentTick & tw.wheelMask

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
