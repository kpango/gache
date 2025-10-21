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

//go:build !goexperiment.swissmap

package gache

import "unsafe"

const (
	// bucketCnt is the number of key/value slots per bucket in the classic
	// (non-swiss) Go hashmap implementation. This MUST match runtime/hashmap.go.
	bucketCnt = 8
)

// NOTE: unsafe.Sizeof is not a constant expression; keep these as vars so
// they are computed at program init on the current architecture.
var (
	// ptrSize is the size, in bytes, of a single pointer-sized word on this arch.
	ptrSize = unsafe.Sizeof(uintptr(0))
	// singleBucketSize is the size of the fixed bmap header (tophash only).
	// Keys, values, and the overflow pointer are laid out after this header.
	singleBucketSize = unsafe.Sizeof(bmap{})
	// sliceHeaderSize is the size of a Go slice header on this arch (3 words).
	sliceHeaderSize = unsafe.Sizeof([]*bmap(nil))
)

// bmap is the fixed-size bucket header used by the classic runtime hashmap.
// It contains only the tophash array. The rest of the bucket's memory layout
// is synthesized as:
//
//	[tophash bucketCnt bytes]                 // sizeof(bmap)
//	[padding to Alignof(K)]
//	[key array: bucketCnt elements of K]      // bucketCnt * Sizeof(K)
//	[padding to Alignof(V)]
//	[val array: bucketCnt elements of V]      // bucketCnt * Sizeof(V)
//	[padding to ptrSize]
//	[overflow pointer: *bmap]                 // ptrSize
//	[final padding to ptrSize]
//
// The final bucket object size is pointer-aligned so an array of buckets is
// itself properly aligned at each element boundary.
type bmap struct {
	tophash [bucketCnt]uint8
}

// hmap mirrors the runtime's classic hashmap header (NOT swissmap).
// Do not rely on field names beyond those used here, and keep this definition
// in sync with runtime/hashmap.go for the target Go version when auditing.
type hmap struct {
	count     int
	flags     uint8
	B         uint8  // log2(#buckets); current bucket array length is 1<<B
	noverflow uint16 // APPROXIMATE overflow bucket count; DO NOT use for exact sizing
	hash0     uint32

	buckets    unsafe.Pointer // *bucket[1<<B], nil until the first insert
	oldbuckets unsafe.Pointer // *bucket[1<<(B-1)] during grow; otherwise nil
	nevacuate  uintptr        // evacuation progress (bucket index)

	clearSeq uint64
	extra    *mapextra // overflow bookkeeping (slices + free-list)
}

// mapextra tracks overflow buckets for the current and old bucket arrays,
// plus a singly-linked free-list for future use. These structures OWN memory
// (slice backing arrays and overflow buckets) that must be attributed to the map.
type mapextra struct {
	overflow     *[]*bmap // overflow buckets currently associated with buckets
	oldoverflow  *[]*bmap // overflow buckets associated with oldbuckets during grow
	nextOverflow *bmap    // free-list of overflow buckets (singly-linked via tail ptr)
}

// overflowSizes computes sizes attributable to overflow bookkeeping and objects.
//
// Returns:
//
//	metaSize:    sum of slice metadata (headers) and backing arrays for both
//	             overflow and oldoverflow slices (capacity * sizeof(*bmap)).
//	bucketsSize: sum of ALL distinct overflow bucket objects, including those
//	             referenced from both slices and those chained in nextOverflow.
//
// Correctness notes:
//   - DO NOT use h.noverflow: it is approximate. Count from actual structures.
//   - Deduplicate bucket objects by address to avoid double-counting across
//     slices and the free-list.
//   - The free-list is a singly-linked chain via the overflow pointer at the
//     tail of each bucket object. We still guard against cycles defensively.
//   - A common case is "no overflow anywhere"; we take a fast path for that.
func overflowSizes(extra *mapextra, oneBucketSize uintptr) (metaSize, bucketsSize uintptr) {
	if extra == nil {
		return 0, 0
	}
	// Fast path: nothing to account for.
	if extra.overflow == nil && extra.oldoverflow == nil && extra.nextOverflow == nil {
		return 0, 0
	}

	// Capacity estimate for dedup maps to reduce rehashing.
	est := 0
	if extra.overflow != nil {
		est += len(*extra.overflow)
	}
	if extra.oldoverflow != nil {
		est += len(*extra.oldoverflow)
	}
	if est < 8 {
		est = 8
	}

	// Dedup sets.

	// "seen" remembers any overflow bucket we've already counted (by address),
	// regardless of where we found it (slices or free-list).
	seen := make(map[uintptr]struct{}, est) // buckets counted (from any path)
	// Account slice metadata + backing arrays for []*bmap slices, and add each
	// distinct bucket from those slices to the total.
	accountSlice := func(sp *[]*bmap) {
		if sp == nil {
			return
		}
		s := *sp
		// Slice header + backing array capacity (pointer-sized elements).
		metaSize += sliceHeaderSize + uintptr(cap(s))*ptrSize
		for i := 0; i < len(s); i++ {
			b := s[i]
			if b == nil {
				continue
			}
			addr := uintptr(unsafe.Pointer(b))
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			bucketsSize += oneBucketSize
		}
	}

	accountSlice(extra.overflow)
	accountSlice(extra.oldoverflow)

	// "freeVisited" records nodes visited while following the free-list to avoid
	// cycles (defensive—should not happen) and accidental infinite walks.
	freeVisited := make(map[uintptr]struct{}, est)

	// Walk the free-list starting at nextOverflow. We always traverse until the
	// end (or a detected cycle) even if we encounter nodes already counted via
	// the slices: we still need to reach subsequent nodes in the chain.
	for b := extra.nextOverflow; b != nil; {
		addr := uintptr(unsafe.Pointer(b))
		// Defensive cycle detection.
		if _, cyc := freeVisited[addr]; cyc {
			break
		}
		freeVisited[addr] = struct{}{}

		// Count if not already seen via slices or earlier in the chain.
		if _, ok := seen[addr]; !ok {
			seen[addr] = struct{}{}
			bucketsSize += oneBucketSize
		}

		// Load next via the overflow pointer located at the tail of the bucket object:
		//   next = *(**bmap)((byte*)b + oneBucketSize - ptrSize)
		nextPtr := (*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(b), oneBucketSize-uintptr(ptrSize)))
		b = (*bmap)(*nextPtr)
	}
	clear(seen)
	seen = nil
	clear(freeVisited)
	freeVisited = nil

	return metaSize, bucketsSize
}

// mapSize estimates the number of bytes OWNED by the map's internal structures.
//
// INCLUDED (owned by the map):
//   - hmap header object
//   - mapextra object (if present)
//   - current bucket array: (1<<B) * bucketObjectSize, present after the first insert
//   - old bucket array during growth: (1<<(B-1)) * bucketObjectSize (or 1 if B==0)
//   - ALL distinct overflow bucket objects (both used and on the free-list)
//   - bookkeeping metadata for overflow slices: slice headers and their backing arrays
//
// EXCLUDED (not owned by the map itself):
//   - Any separately-allocated memory referenced by keys/values (e.g., string or []byte
//     backing stores, or objects behind pointers). Including those would double-count
//     application data that is outside of the map's internal storage.
//
// ZERO BUCKET NOTE:
//   - The runtime may consult a globally shared "zero bucket" for empty maps
//     (h.count==0 && h.buckets==nil). That memory is NOT owned per map instance,
//     so we intentionally DO NOT attribute it here. This yields the correct accounting
//     for "bytes owned by this map".
//
// CONCURRENCY:
//   - This function peeks at runtime internals via unsafe. It must not be called
//     concurrently with mutations to the map.
//
// BUILD COMPATIBILITY:
//   - This file targets the classic hashmap only: //go:build !goexperiment.swissmap
//     When the swissmap experiment is enabled, bucket layout differs; provide a
//     separate implementation under the corresponding build tag.
func mapSize[K comparable, V any](m map[K]V) uintptr {
	if m == nil {
		return 0
	}
	// Extract *hmap from the map value.
	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		return 0
	}

	// Start with the hmap header size.
	size := unsafe.Sizeof(*h)

	// If mapextra exists, add the struct itself. (Its slices/backing arrays are accounted below.)
	if h.extra != nil {
		size += unsafe.Sizeof(*h.extra)
	}

	// Synthesize the bucket object size for the concrete K, V types.
	// Strictly follow the classic runtime layout: keys array, then values array,
	// then the overflow pointer, with alignment between regions and at the end.
	var (
		k K
		v V
	)
	ks, vs := unsafe.Sizeof(k), unsafe.Sizeof(v)
	ka, va := unsafe.Alignof(k), unsafe.Alignof(v)

	off := singleBucketSize     // tophash
	off = alignUp(off, ka)      // align to key alignment
	off += bucketCnt * ks       // keys array
	off = alignUp(off, va)      // align to value alignment
	off += bucketCnt * vs       // values array
	off = alignUp(off, ptrSize) // align to pointer alignment for overflow ptr
	off += ptrSize              // overflow pointer (*bmap)
	off = alignUp(off, ptrSize) // final pointer alignment for array element layout
	bucketObjSize := off

	// Current bucket array: present after the first insert (for B==0, exactly 1 bucket).
	if h.buckets != nil {
		n := uintptr(1) << h.B
		size += n * bucketObjSize
	}

	// Old bucket array during growth: half the size. For the B==0 edge case, treat as 1 bucket.
	if h.oldbuckets != nil {
		nOld := uintptr(1)
		if h.B > 0 {
			nOld = uintptr(1) << (h.B - 1)
		}
		size += nOld * bucketObjSize
	}

	// Overflow bookkeeping and objects (deduplicated across slices and free-list).
	meta, overflows := overflowSizes(h.extra, bucketObjSize)
	size += meta + overflows

	// The shared zero bucket (used by empty maps) is intentionally ignored: not owned by this map.
	return size
}
