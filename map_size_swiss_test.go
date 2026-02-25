package gache

import (
	"testing"
	"unsafe"
)

func TestMapSizeWithSwiss(t *testing.T) {
	t.Run("zero-sized value alignment", func(t *testing.T) {
		m := make(map[int64]struct{})
		m[0] = struct{}{} // Trigger allocation

		var k int64
		var v struct{}
		keySize, keyAlign := unsafe.Sizeof(k), unsafe.Alignof(k)
		valSize, valAlign := unsafe.Sizeof(v), unsafe.Alignof(v)

		// ctrl byte + 8 slots.
		// The values are aligned to their natural alignment.
		// The keys are not aligned; they are just packed together.
		// The ctrl bytes are at the beginning of the group.
		valOffset := alignUp(keySize, valAlign)
		slotSize := valOffset + valSize
		if valSize == 0 {
			// Zero-sized values take up 1 byte for the value, but the overall
			// slot size is aligned up to the key alignment.
			slotSize = alignUp(keySize+1, keyAlign)
		}

		// For int64, keySize is 8, keyAlign is 8.
		// For struct{}, valSize is 0, valAlign is 1.
		// valOffset = alignUp(8, 1) = 8
		// slotSize = 8 + 0 = 8.
		// Because valSize is 0, we do alignUp(keySize+1, keyAlign) = alignUp(9, 8) = 16.
		expectedSlotSize := uintptr(16)

		if slotSize != expectedSlotSize {
			t.Errorf("expected slot size to be %d for map[int64]struct{}, but got %d", expectedSlotSize, slotSize)
		}
	})
}
