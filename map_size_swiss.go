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

import (
	"reflect"
	"unsafe"
)

// Allocator constants and size classes of the Go runtime, read in Go 1.27.3:
// internal/runtime/gc/sizeclasses.go (SizeClassToSize, MaxSmallSize, TinySize,
// PageShift) and internal/runtime/gc/malloc.go (MallocHeaderSize,
// MinSizeForMallocHeader = PtrSize * PtrBits). runtime/msize.go roundupsize
// applies them the way allocSize does.
const (
	tinySize               = 16
	maxSmallSize           = 32768
	pageSize               = 8192
	mallocHeaderSize       = 8
	minSizeForMallocHeader = wordSize * wordSize * 8
)

// sizeClasses lists the class sizes. classBySize8 and classBySize128 map a size,
// in 8-byte steps up to smallSizeMax and in 128-byte steps above, to the
// smallest class that holds it (SizeToSizeClass8 and SizeToSizeClass128 in the
// runtime), so allocSize is one load. classBySize8 holds each class divided by
// smallSizeDiv in a constant string, so that the compiler folds the lookup of a
// constant size (a struct's unsafe.Sizeof) to a constant; TestAllocSize checks it
// against sizeClasses. classBySize128 is built from sizeClasses.
var sizeClasses = [...]uint16{
	8,
	16,
	24,
	32,
	48,
	64,
	80,
	96,
	112,
	128,
	144,
	160,
	176,
	192,
	208,
	224,
	240,
	256,
	288,
	320,
	352,
	384,
	416,
	448,
	480,
	512,
	576,
	640,
	704,
	768,
	896,
	1024,
	1152,
	1280,
	1408,
	1536,
	1792,
	2048,
	2304,
	2688,
	3072,
	3200,
	3456,
	4096,
	4864,
	5376,
	6144,
	6528,
	6784,
	6912,
	8192,
	9472,
	9728,
	10240,
	10880,
	12288,
	13568,
	14336,
	16384,
	18432,
	19072,
	20480,
	21760,
	24576,
	27264,
	28672,
	32768,
}

const (
	smallSizeMax = 1024
	smallSizeDiv = 8
	largeSizeDiv = 128
)

const classBySize8 = "\x01\x01\x02\x03\x04\x06\x06\x08\x08\x0a\x0a\x0c\x0c\x0e\x0e\x10\x10\x12\x12\x14\x14\x16\x16\x18\x18\x1a\x1a\x1c\x1c\x1e\x1e\x20\x20\x24\x24\x24\x24\x28\x28\x28\x28\x2c\x2c\x2c\x2c\x30\x30\x30\x30\x34\x34\x34\x34\x38\x38\x38\x38\x3c\x3c\x3c\x3c\x40\x40\x40\x40\x48\x48\x48\x48\x48\x48\x48\x48\x50\x50\x50\x50\x50\x50\x50\x50\x58\x58\x58\x58\x58\x58\x58\x58\x60\x60\x60\x60\x60\x60\x60\x60\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x70\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80\x80"

var classBySize128 = func() (by128 [(maxSmallSize-smallSizeMax)/largeSizeDiv + 1]uint16) {
	c := 0
	for i := range by128 {
		for int(sizeClasses[c]) < smallSizeMax+i*largeSizeDiv {
			c++
		}
		by128[i] = sizeClasses[c]
	}
	return by128
}()

// allocSize estimates the heap bytes one allocation of size bytes takes: its size
// class, plus the malloc header a pointerful (scan) object larger than
// minSizeForMallocHeader carries, or whole pages for a large object. A pointer-free
// object smaller than tinySize shares a tinySize block with others (the runtime's
// tiny allocator, malloc.go): it is charged its share of a block packed with
// objects of its own size, which is exact while they stay alive together.
func allocSize(size uintptr, scan bool) uintptr {
	switch {
	case size == 0:
		return 0
	case !scan && size < tinySize:
		// The runtime aligns a tiny object to 8, 4 or 2 when size is a multiple of
		// it, so same-size objects pack back to back: tinySize/size per block.
		return tinySize / (tinySize / size)
	case size <= maxSmallSize-mallocHeaderSize:
		if scan && size > minSizeForMallocHeader {
			size += mallocHeaderSize
		}
		if size <= smallSizeMax-8 { // the runtime's bound (runtime/msize.go)
			return uintptr(classBySize8[(size+smallSizeDiv-1)/smallSizeDiv]) * smallSizeDiv
		}
		return uintptr(classBySize128[(size-smallSizeMax+largeSizeDiv-1)/largeSizeDiv])
	default:
		return alignUp(size, pageSize)
	}
}

// alignUp returns the smallest multiple of a, a power of two, not below val.
func alignUp(val, a uintptr) uintptr {
	return (val + a - 1) &^ (a - 1)
}

// scanMatters reports whether allocSize(size, scan) can depend on scan for a
// type of that size: above minSizeForMallocHeader a scan object carries a malloc
// header. Below tinySize a pointerful type's size is a multiple of wordSize, and
// sizes 8 and 12 are charged alike as a size class and as a tiny share; only a
// one-word type on 32-bit (4 bytes: class 8 or a tiny share of 4) differs.
func scanMatters(size uintptr) bool {
	return size > minSizeForMallocHeader || size < 8 && size == wordSize
}

// typeHasPointers reports whether a T contains pointers, so that the GC scans
// it, without walking the type: it reads PtrBytes, the second word of the
// runtime type descriptor behind reflect.Type (reflect.rtype embeds
// internal/abi.Type, whose first fields are Size_ uintptr and PtrBytes uintptr;
// Go 1.27.3 internal/abi/type.go). It allocates nothing.
func typeHasPointers[T any]() bool {
	t := reflect.TypeFor[T]()
	desc := (*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1]
	return (*[2]uintptr)(desc)[1] != 0
}

// hmap mirrors internal/runtime/maps.Map (Go 1.24+ swissmap default).
// Keep in sync with internal/runtime/maps/map.go.
//
// 64-bit layout (size = 48 bytes):
//
//	used              uint64         offset  0
//	seed              uintptr        offset  8
//	dirPointer            unsafe.Pointer offset 16
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
	dirPointer        unsafe.Pointer
	dirLen            int
	globalDepth       uint8
	globalShift       uint8
	writing           uint8
	tombstonePossible bool
	_                 [4]byte
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
	_pad       uint8
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
	return mapSizeScan(m, typeHasPointers[struct {
		key  K
		elem V
	}]())
}

// mapSizeScan is mapSize for a caller that knows slotScan, whether a slot (a K
// and a V) has pointers, without reading the type descriptor: a shardMap's
// buckets hold *valueOf values, so their slots always do.
func mapSizeScan[K comparable, V any](m map[K]V, slotScan bool) uintptr {
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
		slots [8]slot
		ctrl  uint64
	}

	groupSize := unsafe.Sizeof(group{})

	// Every allocation is charged as allocSize rounds it: the hmap, the group of a
	// small map, the directory array, and per table its header and groups array.
	size := allocSize(unsafe.Sizeof(*h), true)

	if h.dirLen == 0 {
		// Small-map optimisation: dirPointer points directly to one group.
		if h.dirPointer != nil {
			size += allocSize(groupSize, slotScan)
		}
		return size
	}

	// Large map: dirPointer is *[dirLen]*table.
	// Account for the directory pointer array itself.
	const ptrSize = unsafe.Sizeof(uintptr(0))
	// dirLen is a power of two (NewMap rounds it with alignUpPow2, growToTable
	// makes one entry, installTableSplit doubles it), and every power of two from
	// 8 to minSizeForMallocHeader is a size class, so only other sizes are rounded.
	dirSize := uintptr(h.dirLen) * ptrSize
	if dirSize < 8 || dirSize > minSizeForMallocHeader {
		dirSize = allocSize(dirSize, true)
	}
	size += dirSize

	// Directory entries may alias one table, but the entries of one table are
	// contiguous (extendible hashing), so a table is counted when its pointer
	// differs from the previous entry's. The runtime's code is cited by function,
	// not by line, since the lines move between Go releases (read in Go 1.27.3's
	// internal/runtime/maps; Go 1.26.1 has the same structure at other lines):
	// directorySet is called only by replaceTable, which fills the block
	// nt.index .. nt.index+(1<<(globalDepth-nt.localDepth)) with nt; replaceTable
	// runs for the two halves of a split (installTableSplit: left.index =
	// old.index, right.index = left.index + one block) and for a table grown in
	// place (table.grow). The directory only doubles by copying every entry to two
	// adjacent slots (installTableSplit), so blocks stay contiguous; NewMap gives
	// each entry its own table and growToTable makes a one-entry directory. The
	// runtime itself relies on this in Map.Clear and Map.Clone.
	tableHdrSize := allocSize(unsafe.Sizeof(tableHdr{}), true)
	// Most tables share a capacity, so the rounded size of a groups array is
	// recomputed only when the group count changes.
	var prev unsafe.Pointer
	var groups uint64
	var groupsSize uintptr
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Add(h.dirPointer, uintptr(i)*ptrSize))
		if tp == nil || tp == prev {
			continue
		}
		prev = tp
		t := (*tableHdr)(tp)
		// tableHdr struct itself, plus the groups backing array: (lengthMask+1) groups.
		if n := t.groups.lengthMask + 1; n != groups {
			groups, groupsSize = n, allocSize(uintptr(n)*groupSize, slotScan)
		}
		size += tableHdrSize + groupsSize
	}

	return size
}
