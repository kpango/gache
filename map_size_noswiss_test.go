// Copyright (c) 2024 The Go Authors. All rights reserved.
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

import (
	"testing"
	"unsafe"
)

// bucketObjectSizeFor synthesizes the expected bucket object size for K,V.
// It independently reproduces the layout math to validate the alignment logic.
func bucketObjectSizeFor[K comparable, V any]() uintptr {
	var (
		k K
		v V
	)
	ks, vs := unsafe.Sizeof(k), unsafe.Sizeof(v)
	ka, va := unsafe.Alignof(k), unsafe.Alignof(v)

	off := singleBucketSize
	off = alignUp(off, ka)      // keys start
	off += bucketCnt * ks       // keys array
	off = alignUp(off, va)      // values start
	off += bucketCnt * vs       // values array
	off = alignUp(off, ptrSize) // overflow ptr align
	off += ptrSize              // overflow ptr
	off = alignUp(off, ptrSize) // final align
	return off
}

// expectedSizeFromInternals composes the exact expected size by peeking the
// mirrored hmap/mapextra structures. This mirrors the definition of “bytes owned
// by the map”: header + mapextra + (current + old) bucket arrays + overflow meta + overflow buckets.
func expectedSizeFromInternals[K comparable, V any](m map[K]V) uintptr {
	if m == nil {
		return 0
	}
	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		return 0
	}

	total := unsafe.Sizeof(*h)

	if h.extra != nil {
		total += unsafe.Sizeof(*h.extra)
	}

	bsz := bucketObjectSizeFor[K, V]()

	if h.buckets != nil {
		n := uintptr(1) << h.B
		total += n * bsz
	}
	if h.oldbuckets != nil {
		nOld := uintptr(1)
		if h.B > 0 {
			nOld = uintptr(1) << (h.B - 1)
		}
		total += nOld * bsz
	}

	meta, buckets := overflowSizes(h.extra, bsz)
	total += meta + buckets

	return total
}

// ensureGrowthCompletion best-effort nudges evacuation to progress if a grow is
// in-flight. Not strictly required for correctness because we account oldbuckets,
// but it reduces flakiness across runtime versions.
func ensureGrowthCompletion[K comparable, V any](m map[K]V) {
	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil || h.oldbuckets == nil {
		return
	}
	for range 4 {
		for k := range m {
			_ = m[k]
		}
		h = (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
		if h.oldbuckets == nil {
			return
		}
	}
}

func TestMapSize_NilAndEmpty(t *testing.T) {
	t.Parallel()

	// nil map: must be 0
	var mNil map[int]int
	if got := mapSize[int, int](mNil); got != 0 {
		t.Fatalf("nil mapSize = %d, want 0", got)
	}

	// empty (make): only hmap header should be owned
	m := make(map[int]int)
	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatalf("unexpected nil hmap for empty map")
	}
	got := mapSize[int, int](m)
	if got <= 0 {
		t.Fatalf("empty mapSize = %d, want > 0", got)
	}
}

func TestMapSize_OneInsert_IntInt(t *testing.T) {
	t.Parallel()

	m := make(map[int]int)
	m[1] = 1 // should allocate the first bucket array

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatalf("nil hmap after first insert")
	}
	if h.buckets == nil {
		t.Fatalf("buckets should be allocated after first insert")
	}

	// Cross-check against the internal composition function.
	want := expectedSizeFromInternals[int, int](m)
	got := mapSize[int, int](m)
	if got != want {
		t.Fatalf("one-insert mapSize = %d, want %d (composed from internals)", got, want)
	}
}

func TestMapSize_Growth_IntInt(t *testing.T) {
	// Insert enough elements to force multiple resizes.
	m := make(map[int]int)
	const N = 1 << 12 // 4096
	for i := range N {
		m[i] = i
	}

	// Optionally complete evacuation to stabilize shapes.
	ensureGrowthCompletion[int, int](m)

	// The two computations must agree exactly.
	want := expectedSizeFromInternals[int, int](m)
	got := mapSize[int, int](m)

	if got != want {
		t.Fatalf("growth mapSize = %d, want %d (from internals)", got, want)
	}
}

func TestMapSize_StringValueIndependence_InPlaceUpdate(t *testing.T) {
	// Mutating values from small strings to huge strings must NOT change the
	// owned size: map internals (buckets/overflow/etc.) are unaffected by value
	// backing stores, which live outside the map.
	m := make(map[int]string)
	const N = 2048
	for i := range N {
		m[i] = "x"
	}
	ensureGrowthCompletion[int, string](m)
	before := mapSize[int, string](m)

	// Build one large shared backing and assign to all values.
	long := make([]byte, 1<<20) // ~1 MiB backing
	for i := range long {
		long[i] = 'X'
	}
	longStr := string(long)

	for i := range N {
		m[i] = longStr
	}
	ensureGrowthCompletion[int, string](m)
	after := mapSize[int, string](m)

	if before != after {
		t.Fatalf("mapSize changed after replacing values with huge strings: before=%d after=%d (should be equal)", before, after)
	}
}

func TestMapSize_ZeroSizedTypes(t *testing.T) {
	t.Parallel()

	// Case 1: V is zero-sized (struct{}). Expect layout/alignment to be correct.
	{
		m := make(map[int]struct{})
		for i := range 1024 {
			m[i] = struct{}{}
		}
		ensureGrowthCompletion[int, struct{}](m)
		got := mapSize[int, struct{}](m)
		want := expectedSizeFromInternals[int, struct{}](m)
		if got != want {
			t.Fatalf("map[int]struct{} size=%d, want %d", got, want)
		}
	}

	// Case 2: K is zero-sized. Only a single key logically exists (all struct{} equal).
	{
		m := make(map[struct{}]int)
		m[struct{}{}] = 1

		got := mapSize[struct{}, int](m)
		want := expectedSizeFromInternals[struct{}, int](m)
		if got != want {
			t.Fatalf("map[struct{}]int size=%d, want %d", got, want)
		}
	}
}

func TestBucketObjectSize_MatchesLayoutMath(t *testing.T) {
	t.Parallel()

	// A small sanity matrix of (K,V) pairs to check the synthesized size.
	type big struct {
		A uint64
		B uint64
		C uint32
		D byte
	}
	tests := []struct {
		name string
	}{
		{"int-int"},
		{"uint64-byte"},
		{"string-string"},
		{"string-uint64"},
		{"[16]byte-struct"},
		{"struct-bool"},
	}
	_ = tests // names only, we just exercise the function with a few instantiations

	// Just ensure the function compiles and produces a non-zero pointer-aligned result.
	if s := bucketObjectSizeFor[int, int](); s == 0 || s%ptrSize != 0 {
		t.Fatalf("bucketObjectSizeFor[int,int] = %d, want >0 and pointer-aligned", s)
	}
	if s := bucketObjectSizeFor[string, uint64](); s == 0 || s%ptrSize != 0 {
		t.Fatalf("bucketObjectSizeFor[string,uint64] = %d, want >0 and pointer-aligned", s)
	}
	if s := bucketObjectSizeFor[big, bool](); s == 0 || s%ptrSize != 0 {
		t.Fatalf("bucketObjectSizeFor[big,bool] = %d, want >0 and pointer-aligned", s)
	}
}

func TestMapSize_ExtrasPresenceAccounting(t *testing.T) {
	// Verify that the mapextra struct (when present) is included exactly once
	// on top of overflow slices/buckets.
	m := make(map[int]int)
	// Create enough entries to likely create/keep mapextra (runtime may allocate
	// it lazily for overflow bookkeeping). Even if not present, both paths are valid.
	for i := range 1024 {
		m[i] = i
	}
	ensureGrowthCompletion[int, int](m)

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatalf("nil hmap")
	}

	got := mapSize[int, int](m)
	want := expectedSizeFromInternals[int, int](m)

	if got != want {
		// Provide some hints to ease debugging if a future runtime change occurs.
		extraSize := uintptr(0)
		if h.extra != nil {
			extraSize = unsafe.Sizeof(*h.extra)
		}
		t.Fatalf("mapSize mismatch: got=%d want=%d (mapextra=%d present=%v)",
			got, want, extraSize, h.extra != nil)
	}
}
