package gache

import (
	"sync"
	"time"
)

type wheelItem struct {
	key    string
	expire int64
}

type timingWheel struct {
	buckets    [][]wheelItem
	mu         sync.Mutex
	resolution int64
	mask       int64
	lastTick   int64
}

func newTimingWheel(bits uint8, resolution time.Duration, now int64) *timingWheel {
	size := 1 << bits
	return &timingWheel{
		buckets:    make([][]wheelItem, size),
		resolution: resolution.Nanoseconds(),
		mask:       int64(size - 1),
		lastTick:   now / resolution.Nanoseconds(),
	}
}

func (tw *timingWheel) add(key string, expire int64) {
	if expire <= 0 {
		return
	}
	tick := expire / tw.resolution
	idx := tick & tw.mask
	tw.mu.Lock()
	tw.buckets[idx] = append(tw.buckets[idx], wheelItem{key: key, expire: expire})
	tw.mu.Unlock()
}

func (tw *timingWheel) advance(now int64) []wheelItem {
	currentTick := now / tw.resolution

	tw.mu.Lock()
	defer tw.mu.Unlock()

	ticks := currentTick - tw.lastTick
	if ticks <= 0 {
		return nil
	}

	size := int64(len(tw.buckets))
	if ticks > size {
		ticks = size
		tw.lastTick = currentTick - size
	}

	var expired []wheelItem
	for i := int64(0); i < ticks; i++ {
		tw.lastTick++
		idx := tw.lastTick & tw.mask
		bucket := tw.buckets[idx]
		if len(bucket) == 0 {
			continue
		}
		var remaining []wheelItem
		for _, item := range bucket {
			if item.expire <= now {
				expired = append(expired, item)
			} else {
				remaining = append(remaining, item)
			}
		}
		tw.buckets[idx] = remaining
	}

	return expired
}
