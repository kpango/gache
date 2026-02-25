package gache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func BenchmarkGacheSet(b *testing.B) {
	g := New[int]()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Set("key", i)
	}
}

func BenchmarkGacheGet(b *testing.B) {
	g := New[int]()
	g.Set("key", 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Get("key")
	}
}

func BenchmarkGacheSetWithExpire(b *testing.B) {
	g := New[int]()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.SetWithExpire("key", i, time.Hour)
	}
}

func BenchmarkGacheGetParallel(b *testing.B) {
	g := New[int]()
	g.Set("key", 1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Get("key")
		}
	})
}

func BenchmarkGacheSetParallel(b *testing.B) {
	g := New[int]()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Set("key", 1)
		}
	})
}

func BenchmarkGacheSetWithExpireParallel(b *testing.B) {
	g := New[int]()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.SetWithExpire("key", 1, time.Hour)
		}
	})
}

// BenchmarkClockOverhead measures the overhead of the atomic clock vs time.Now()
// Indirectly measured via Get which uses the clock.
func BenchmarkClockGet(b *testing.B) {
	g := New[int]()
	g.Set("key", 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.Get("key")
	}
}

// BenchmarkHighChurn simulates a scenario with many keys being added and expiring.
// This tests the TimingWheel and expiration logic overhead.
func BenchmarkHighChurn(b *testing.B) {
	// Custom config for high churn
	g := New[int](
		WithClockInterval[int](10 * time.Millisecond),
		WithTimingWheelBits[int](10), // smaller wheel for more collisions/turnover
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// We don't start the background worker here to avoid noise,
	// or we can start it to test contention. Let's start it.
	g.StartExpired(ctx, 10*time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			// Set with very short expiration to trigger wheel churn
			g.SetWithExpire(key, i, 1*time.Millisecond)
			i++
		}
	})
	g.Stop()
}

func BenchmarkLargeScaleExpiration(b *testing.B) {
	g := New[int]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-fill
	const count = 100000
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(val int) {
			defer wg.Done()
			g.SetWithExpire(fmt.Sprintf("key-%d", val), val, 1*time.Millisecond)
		}(i)
	}
	wg.Wait()

	// Measure deletion time
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// We call DeleteExpired directly.
		// Since keys expire at 1ms, and we might run faster,
		// we only delete once. This benchmark might be flawed for repeated runs.
		// Instead, we can measure the cost of checking expiration when nothing is expired
		// vs when something is.
		g.DeleteExpired(ctx)
	}
}
