// Copyright (c) 2009 The Go Authors. All rights reserved.
// Modified by kpango.

// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:

//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.

// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package gache

import "unsafe"

// hmap mirrors internal/runtime/maps.Map (Go 1.24+ swissmap default).
// Keep in sync with internal/runtime/maps/map.go.
//
// 64-bit layout (size = 48 bytes):
//
//	used              uint64         offset  0
//	seed              uintptr        offset  8
//	dirPtr            unsafe.Pointer offset 16
//	dirLen            int            offset 24
//	globalDepth       uint8          offset 32
//	globalShift       uint8          offset 33
//	writing           uint8          offset 34
//	tombstonePossible bool           offset 35
//	(4 bytes implicit padding)
//	clearSeq          uint64         offset 40
type hmap struct {
	used              uint64
	seed              uintptr
	dirPtr            unsafe.Pointer
	dirLen            int
	globalDepth       uint8
	globalShift       uint8
	writing           uint8
	tombstonePossible bool
	clearSeq          uint64
}

// tableHdr mirrors internal/runtime/maps.table (Go 1.24+ swissmap).
// Keep in sync with internal/runtime/maps/table.go.
//
// 64-bit layout (size = 32 bytes):
//
//	used       uint16         offset  0
//	capacity   uint16         offset  2
//	growthLeft uint16         offset  4
//	localDepth uint8          offset  6
//	(1 byte implicit padding)
//	index      int            offset  8
//	groups     groupsRef      offset 16
type tableHdr struct {
	used       uint16
	capacity   uint16
	growthLeft uint16
	localDepth uint8
	_pad       uint8 // padding to align index to 8
	index      int
	groups     groupsRef
}

// groupsRef mirrors internal/runtime/maps.groupsReference.
// Keep in sync with internal/runtime/maps/group.go.
type groupsRef struct {
	data       unsafe.Pointer // *[lengthMask+1]group
	lengthMask uint64         // numGroups - 1 (numGroups is always a power of 2)
}

// mapSize estimates the number of bytes owned by the SwissTable map's
// internal structures.
//
// Group layout (conceptually, from internal/runtime/maps/group.go):
//
//	type group struct {
//	    ctrl  uint64               // 8 bytes: one control byte per slot
//	    slots [8]struct{ key K; elem V }
//	}
//
// SlotSize is modeled as sizeof(struct{ key K; elem V }) computed by the
// compiler using Go's usual struct layout rules. The actual runtime swissmap
// implementation may apply additional optimizations (for example, special-
// casing zero-sized values), so mapSize should be understood as an
// approximation/upper bound of the bytes owned by the map rather than an
// exact reflection of every internal optimization.
func mapSize[K comparable, V any](m map[K]V) uintptr {
	if m == nil {
		return 0
	}

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		return 0
	}

	type slot struct {
		key  K
		elem V
	}
	type group struct {
		ctrl  uint64
		slots [8]slot
	}

	groupSize := unsafe.Sizeof(group{})

	size := unsafe.Sizeof(*h)

	if h.dirLen == 0 {
		// Small-map optimisation: dirPtr points directly to one group.
		if h.dirPtr != nil {
			size += groupSize
		}
		return size
	}

	// Large map: dirPtr is *[dirLen]*table.
	// Account for the directory pointer array itself.
	const ptrSize = unsafe.Sizeof(uintptr(0))
	size += uintptr(h.dirLen) * ptrSize

	// Deduplicate table pointers (directory entries may alias the same table).
	tables := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(h.dirPtr) + uintptr(i)*ptrSize))
		if tp != nil {
			tables[tp] = struct{}{}
		}
	}

	tableHdrSize := unsafe.Sizeof(tableHdr{})
	for tp := range tables {
		t := (*tableHdr)(tp)
		// tableHdr struct itself.
		size += tableHdrSize
		// Groups backing array: (lengthMask+1) groups.
		numGroups := uintptr(t.groups.lengthMask + 1)
		size += numGroups * groupSize
	}

	return size
}
