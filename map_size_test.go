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

package gache

import (
	"testing"
	"unsafe"
)

// TestGacheMapSizeWithDeletedEntries verifies that Size() behaves correctly
// when entries are stored and then deleted.
func TestGacheMapSizeWithDeletedEntries(t *testing.T) {
	m := new(Map[int, int])

	sizeEmpty := m.Size()
	if sizeEmpty == 0 {
		t.Errorf("Size() returned 0 for empty Map")
	}

	m.Store(1, 100)
	m.Delete(1)

	// After deleting the only entry, the internal map may still have
	// allocated buckets, so size should still be positive.
	sizeAfterDelete := m.Size()
	if sizeAfterDelete == 0 {
		t.Errorf("Size() returned 0 after Store+Delete")
	}

	// Store many entries to trigger map growth, then verify size increases.
	for i := range 1000 {
		m.Store(i, i)
	}
	sizeLarge := m.Size()
	if sizeLarge <= sizeAfterDelete {
		t.Errorf("Size() should increase after adding many entries: small=%d large=%d", sizeAfterDelete, sizeLarge)
	}
}

func TestGacheMapSize(t *testing.T) {
	gacheMap := new(Map[int, int])
	gacheMap.Store(1, 1)

	size := gacheMap.Size()
	if size <= 0 {
		t.Errorf("gacheMap.Size() should be positive, got %d", size)
	}

	// The size should at least include the Map struct itself.
	minSize := unsafe.Sizeof(*gacheMap)
	if size < minSize {
		t.Errorf("gacheMap.Size() %d is smaller than expected lower bound %d", size, minSize)
	}
}
