package gache

import (
	"fmt"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestMapSizeCorrectness(t *testing.T) {
	m := &Map[int, int]{}

	// Size empty
	emptySize := m.Size()
	if emptySize <= 0 {
		t.Fatalf("expected empty size to be > 0, got %d", emptySize)
	}

	for i := range 1000 {
		m.Store(i, i)
	}

	// Calculate manually
	var expectedSize uintptr
	expectedSize = unsafe.Sizeof(*m)
	if m.l.Load() != nil {
		expectedSize += unsafe.Sizeof(atomic.Int64{})
	}
	if ro := m.read.Load(); ro != nil {
		expectedSize += unsafe.Sizeof(ro.amended)
		expectedSize += mapSize(ro.m)
		if l := len(ro.m); l > 0 {
			expectedSize += uintptr(l) * (unsafe.Sizeof(entry[int]{}) + unsafe.Sizeof(int(0)))
		}
	}
	expectedSize += mapSize(m.dirty)
	if l := len(m.dirty); l > 0 {
		expectedSize += uintptr(l) * (unsafe.Sizeof(entry[int]{}) + unsafe.Sizeof(int(0)))
	}

	actualSize := m.Size()
	if actualSize != expectedSize {
		t.Fatalf("expected size %d, got %d", expectedSize, actualSize)
	}
}

func TestMapSizeStructCorrectness(t *testing.T) {
	type ComplexStruct struct {
		D map[string]int
		B string
		C []byte
		A int64
	}

	m := &Map[string, ComplexStruct]{}

	for i := range 1000 {
		m.Store(fmt.Sprintf("key-%d", i), ComplexStruct{})
	}

	var expectedSize uintptr
	expectedSize = unsafe.Sizeof(*m)
	if m.l.Load() != nil {
		expectedSize += unsafe.Sizeof(atomic.Int64{})
	}
	if ro := m.read.Load(); ro != nil {
		expectedSize += unsafe.Sizeof(ro.amended)
		expectedSize += mapSize(ro.m)
		if l := len(ro.m); l > 0 {
			expectedSize += uintptr(l) * (unsafe.Sizeof(entry[ComplexStruct]{}) + unsafe.Sizeof(ComplexStruct{}))
		}
	}
	expectedSize += mapSize(m.dirty)
	if l := len(m.dirty); l > 0 {
		expectedSize += uintptr(l) * (unsafe.Sizeof(entry[ComplexStruct]{}) + unsafe.Sizeof(ComplexStruct{}))
	}

	actualSize := m.Size()
	if actualSize != expectedSize {
		t.Fatalf("expected size %d, got %d", expectedSize, actualSize)
	}
}

func BenchmarkMapSize(b *testing.B) {
	m := &Map[int, int]{}
	for i := range 10000 {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

func BenchmarkMapSizeOnlyDirty(b *testing.B) {
	m := &Map[int, int]{}
	m.Store(0, 0)
	for i := 1; i < 10000; i++ {
		m.Store(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

func BenchmarkMapSize_Items_10(b *testing.B) {
	m := &Map[int, int]{}
	for i := range 10 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

func BenchmarkMapSize_Items_100(b *testing.B) {
	m := &Map[int, int]{}
	for i := range 100 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

func BenchmarkMapSize_Items_1000(b *testing.B) {
	m := &Map[int, int]{}
	for i := range 1000 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}

func BenchmarkMapSize_Items_10000(b *testing.B) {
	m := &Map[int, int]{}
	for i := range 10000 {
		m.Store(i, i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Size()
	}
}
