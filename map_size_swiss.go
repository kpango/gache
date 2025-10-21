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

//go:build goexperiment.swissmap

package gache

import "unsafe"

// A header for a Go map.
type hmap struct {
	count  uintptr
	flags  uint8
	B      uint8
	hash0  uint32
	dir    unsafe.Pointer
	dirLen uintptr
}

// A directory entry.
type dirEntry struct {
	table unsafe.Pointer
}

// A table.
type table struct {
	groups [1]group
}

// A group.
type group struct {
	ctrl  [8]uint8
	slots [1]uintptr
}

// mapSize estimates the size of a SwissTable map.
func mapSize[K comparable, V any](m map[K]V) uintptr {
	if m == nil {
		return 0
	}

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		return 0
	}

	var k K
	var v V
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
	groupSize := 8 + 8*slotSize

	size := unsafe.Sizeof(*h)

	if h.dirLen == 0 {
		// Small map optimization.
		if h.dir != nil {
			// The map has a single group.
			size += groupSize
		}
		return size
	}

	// Directory
	tables := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := uintptr(0); i < h.dirLen; i++ {
		entry := (*dirEntry)(unsafe.Pointer(uintptr(h.dir) + i*unsafe.Sizeof(dirEntry{})))
		if t := entry.table; t != nil {
			tables[t] = struct{}{}
		}
	}

	tableSize := (uintptr(1) << h.B) * groupSize
	size += uintptr(len(tables)) * tableSize

	return size
}
