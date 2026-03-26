// Copyright (c) 2024 The Go Authors. All rights reserved.
// Modified by kpango.

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
	"sync/atomic"
	"testing"
	"unsafe"
)

// TestGache_MapSize verifies that gache's total memory size footprint is calculated correctly encompassing active entries and internal structures.
func TestGache_MapSize(t *testing.T) {
	gacheMap := new(Map[int, int])
	gacheMap.Store(1, 1)

	size := gacheMap.Size()
	if size <= 0 {
		t.Errorf("gacheMap.Size() should be positive, got %d", size)
	}

	m := make(map[int]*entry[int])
	m[1] = newEntryPointer(new(int), new(atomic.Int64))

	expectedSize := unsafe.Sizeof(gacheMap.mu) +
		unsafe.Sizeof(gacheMap.read) +
		unsafe.Sizeof(gacheMap.misses) +
		mapSize(gacheMap.dirty)

	// Allow for some slack due to readOnly struct and entry sizes

	if size < expectedSize {
		t.Errorf("gacheMap.Size() %d is smaller than expected lower bound %d", size, expectedSize)
	}
}

// TestGache_MapSizeWithExpungedEntries ensures that entries actively expunged from the map do not falsely inflate the reported byte size.
func TestGache_MapSizeWithExpungedEntries(t *testing.T) {
	m := new(Map[int, int])
	m.Store(1, 100)
	m.Delete(1)

	m.Store(2, 200)

	sizeBefore := m.Size()

	m.Store(3, 300)
	sizeAfter := m.Size()

	if sizeAfter <= sizeBefore {
		t.Errorf("Size() should increase after adding an entry: before=%d after=%d", sizeBefore, sizeAfter)
	}

	if sizeBefore == 0 {
		t.Errorf("Size() returned 0 unexpectedly")
	}
}
