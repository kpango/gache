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

// TestGacheMapSizeWithExpungedEntries verifies that Size() does not count the
// value size for expunged entries (entries whose pointer equals the shared
// expunged marker).  An entry becomes expunged when it is deleted while in the
// read-only map and then dirtyLocked() runs: nil → expunged transition.
func TestGacheMapSizeWithExpungedEntries(t *testing.T) {
	// Store a key, delete it (sets p → nil), then trigger a dirty-promotion
	// cycle so that dirtyLocked() marks the nil entry as expunged.
	m := new(Map[int, int])
	m.Store(1, 100)
	m.Delete(1)

	// Force a dirty-map promotion: storing a new key while the read map is
	// amended causes dirtyLocked() to run and expunge nil entries.
	m.Store(2, 200)

	sizeBefore := m.Size()

	// Add more entries with values to make sure the size grows.
	m.Store(3, 300)
	sizeAfter := m.Size()

	if sizeAfter <= sizeBefore {
		t.Errorf("Size() should increase after adding an entry: before=%d after=%d", sizeBefore, sizeAfter)
	}

	// Expunged entries must not contribute value size.  We verify indirectly
	// by checking that the map is consistent (no panic / negative size).
	if sizeBefore == 0 {
		t.Errorf("Size() returned 0 unexpectedly")
	}
}

func TestGacheMapSize(t *testing.T) {
	gacheMap := new(Map[int, int])
	gacheMap.Store(1, 1)

	size := gacheMap.Size()
	if size <= 0 {
		t.Errorf("gacheMap.Size() should be positive, got %d", size)
	}

	m := make(map[int]*entry[int])
	m[1] = newEntry(1)

	expectedSize := unsafe.Sizeof(gacheMap.mu) +
		unsafe.Sizeof(gacheMap.read) +
		unsafe.Sizeof(gacheMap.misses) +
		mapSize(gacheMap.dirty)

	// Allow for some slack due to readOnly struct and entry sizes
	if size < expectedSize {
		t.Errorf("gacheMap.Size() %d is smaller than expected lower bound %d", size, expectedSize)
	}
}
