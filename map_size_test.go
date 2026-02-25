package gache

import (
	"testing"
)

func TestGacheMapSize(t *testing.T) {
	gacheMap := newMap[int, int]()
	gacheMap.Store(1, 1)

	size := gacheMap.Size()
	if size <= 0 {
		t.Errorf("gacheMap.Size() should be positive, got %d", size)
	}
}
