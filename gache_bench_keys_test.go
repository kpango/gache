package gache

import (
	"context"
	"strconv"
	"testing"
)

func BenchmarkKeys(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := 0; i < 100000; i++ {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Keys(ctx)
	}
}

func BenchmarkValues(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := 0; i < 100000; i++ {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Values(ctx)
	}
}

func BenchmarkToRawMap(b *testing.B) {
	ctx := context.Background()
	g := New[int]()
	for i := 0; i < 100000; i++ {
		g.Set(strconv.Itoa(i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.ToRawMap(ctx)
	}
}
