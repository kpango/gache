package gache

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

type refGroupsRef struct {
	data       unsafe.Pointer
	lengthMask uint64
}

type refTableHdr struct {
	used       uint16
	capacity   uint16
	growthLeft uint16
	localDepth uint8
	_pad       uint8
	index      int
	groups     refGroupsRef
}

type refHmap struct {
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

// groupSizeFor returns the size of one swissmap group for (K, V) independently
// of mapSize, mirroring the group layout from internal/runtime/maps/group.go:
//
//	type group struct {
//	    ctrl  uint64
//	    slots [8]struct{ key K; elem V }
//	}
func groupSizeFor[K comparable, V any]() uintptr {
	type slot struct {
		key  K
		elem V
	}
	type group struct {
		slots [8]slot
		ctrl  uint64
	}
	return unsafe.Sizeof(group{})
}

// expectedSizeFromSwissInternals independently composes the expected memory
// footprint of a swissmap by walking the mirrored hmap / tableHdr / groupsRef
// structures. It is a separate reference implementation of mapSize, so a
// discrepancy in mapSize shows up as a mismatch.
// Accounting breakdown:
//
//	hmap header
//	├─ dirLen == 0, dirPointer != nil  →  + 1 group
//	└─ dirLen >  0  →  + dirLen * ptrSize            (directory array)
//	                   + per unique table:
//	                       tableHdr size
//	                       (lengthMask + 1) * groupSize  (groups backing array)
func expectedSizeFromSwissInternals[K comparable, V any](m map[K]V) uintptr {
	if m == nil {
		return 0
	}
	// Cast through the test-local refHmap mirror rather than the production hmap.
	// If the two definitions diverge, reading fields via this type will yield
	// different values, causing the computed total to differ from mapSize.
	h := (*refHmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		return 0
	}

	groupSize := groupSizeFor[K, V]()
	groupScan := typeHasPointers[K]() || typeHasPointers[V]()
	total := allocSize(unsafe.Sizeof(*h), true)

	if h.dirLen == 0 {
		// Small-map fast path: dirPointer points directly to a single group.
		if h.dirPointer != nil {
			total += allocSize(groupSize, groupScan)
		}
		return total
	}

	// Large map: account for the directory pointer slice.
	const ptrSz = unsafe.Sizeof(uintptr(0))
	total += allocSize(uintptr(h.dirLen)*ptrSz, true)

	// Walk each directory entry, deduplicating table pointers (multiple entries
	// in the directory may alias the same table during and after a split).
	seen := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Add(h.dirPointer, uintptr(i)*ptrSz))
		if tp != nil {
			seen[tp] = struct{}{}
		}
	}

	// Use refTableHdr (test-local mirror) to read each table's fields.
	tableHdrSz := unsafe.Sizeof(refTableHdr{})
	for tp := range seen {
		tbl := (*refTableHdr)(tp)
		total += allocSize(tableHdrSz, true)
		// groups backing array: numGroups = lengthMask + 1 (always a power of 2).
		numGroups := uintptr(tbl.groups.lengthMask + 1)
		total += allocSize(numGroups*groupSize, groupScan)
	}

	return total
}

// nudgeSwissGrowth best-effort forces any in-progress table splits to
// complete by iterating the map several times.  Unlike the non-swiss hmap,
// there is no single oldbuckets pointer to check; instead we just iterate a
// few times to drain pending work.
func nudgeSwissGrowth[K comparable, V any](m map[K]V) {
	for range 4 {
		for k := range m {
			_ = m[k]
		}
	}
}

// requireDirLen asserts that the map's directory length is at least minDir,
// skipping the calling test if the runtime chose a different layout.
func requireDirLen(t *testing.T, h *hmap, minDir int) {
	t.Helper()
	if h.dirLen < minDir {
		t.Skipf("swissmap dirLen=%d < %d; runtime chose a different layout — skipping directory path test", h.dirLen, minDir)
	}
}

// TestMap_SizeSwiss_LargeMap_DirectoryCreated: a map of 8192 entries (a directory of tables) is sized exactly like the reference built from its internals.
func TestMap_SizeSwiss_LargeMap_DirectoryCreated(t *testing.T) {
	const N = 1 << 13 // 8192 — well above any single-table capacity threshold

	m := make(map[int]int, N)
	for i := range N {
		m[i] = i
	}
	nudgeSwissGrowth[int, int](m)

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil hmap after large insert")
	}
	requireDirLen(t, h, 1) // skip if the runtime chose a different layout

	got := mapSize[int, int](m)
	want := expectedSizeFromSwissInternals[int, int](m)
	if got != want {
		t.Fatalf("large-map (dirLen=%d) mapSize=%d, want %d (from internals)", h.dirLen, got, want)
	}
}

// TestMap_SizeSwiss_LargeMap_GrowthStages: mapSize equals the internals reference at the listed growth checkpoints from 64 to 8192 entries.
func TestMap_SizeSwiss_LargeMap_GrowthStages(t *testing.T) {
	stages := []int{64, 256, 512, 1024, 2048, 4096, 8192}

	m := make(map[int]int)
	inserted := 0
	for _, target := range stages {
		for inserted < target {
			m[inserted] = inserted
			inserted++
		}
		nudgeSwissGrowth[int, int](m)

		h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
		if h == nil {
			t.Fatalf("nil hmap at stage %d", target)
		}

		got := mapSize[int, int](m)
		want := expectedSizeFromSwissInternals[int, int](m)
		if got != want {
			t.Fatalf("stage %d (dirLen=%d): mapSize=%d, want %d (from internals)",
				target, h.dirLen, got, want)
		}
	}
}

// TestMap_SizeSwiss_LargeMap_StringValueIndependence: replacing the values with a 1 MiB string leaves mapSize unchanged (only the map structure is sized, not what values point at).
func TestMap_SizeSwiss_LargeMap_StringValueIndependence(t *testing.T) {
	const N = 1 << 13

	m := make(map[int]string, N)
	for i := range N {
		m[i] = "x"
	}
	nudgeSwissGrowth[int, string](m)

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil hmap")
	}
	requireDirLen(t, h, 1)

	before := mapSize[int, string](m)
	want := expectedSizeFromSwissInternals[int, string](m)
	if before != want {
		t.Fatalf("before replace: mapSize=%d, want %d (from internals)", before, want)
	}

	// Replace all values with a large shared backing string (~1 MiB).
	long := string(make([]byte, 1<<20))
	for i := range N {
		m[i] = long
	}
	nudgeSwissGrowth[int, string](m)

	after := mapSize[int, string](m)
	wantAfter := expectedSizeFromSwissInternals[int, string](m)
	if after != wantAfter {
		t.Fatalf("after replace: mapSize=%d, want %d (from internals)", after, wantAfter)
	}
	if before != after {
		t.Fatalf("mapSize changed after large-string replace: before=%d after=%d (should be equal)", before, after)
	}
}

// TestMap_SizeSwiss_LargeMap_TableDedup: mapSize equals the internals reference, which counts each table once, whether or not directory entries alias a table in this run.
func TestMap_SizeSwiss_LargeMap_TableDedup(t *testing.T) {
	const N = 1 << 13

	m := make(map[int]int, N)
	for i := range N {
		m[i] = i
	}
	nudgeSwissGrowth[int, int](m)

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil hmap")
	}
	requireDirLen(t, h, 2)

	const ptrSz = unsafe.Sizeof(uintptr(0))
	unique := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Add(h.dirPointer, uintptr(i)*ptrSz))
		if tp != nil {
			unique[tp] = struct{}{}
		}
	}
	numUnique := len(unique)
	if numUnique == h.dirLen {
		t.Logf("dirLen=%d, all entries unique (no aliasing in this run)", h.dirLen)
	} else {
		t.Logf("dirLen=%d, unique tables=%d — aliasing present, dedup exercised", h.dirLen, numUnique)
	}

	got := mapSize[int, int](m)
	want := expectedSizeFromSwissInternals[int, int](m)
	if got != want {
		t.Fatalf("table-dedup: mapSize=%d, want %d (from internals, dirLen=%d, uniqueTables=%d)",
			got, want, h.dirLen, numUnique)
	}
}

// TestMap_SizeSwiss_DirectoryContiguous pins the runtime property mapSize counts
// tables by: the directory entries of one table form a single run, so no table
// pointer reappears after a different one. Random keys split tables at uneven
// depths, which is what makes entries alias in the first place. mapSize must also
// agree with the reference that deduplicates through a Go map.
func TestMap_SizeSwiss_DirectoryContiguous(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	m := make(map[uint64]int)
	aliased := false
	// Checkpoints every 1000 entries: aliasing only exists while some tables have
	// split and others have not.
	for target := 1000; target <= 1<<17; target += 1000 {
		for len(m) < target {
			m[rng.Uint64()] = 0
		}
		nudgeSwissGrowth[uint64, int](m)

		h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
		if h.dirLen == 0 {
			t.Skip("swissmap small-map layout: no directory")
		}
		seen := make(map[unsafe.Pointer]int, h.dirLen)
		var prev unsafe.Pointer
		for i := 0; i < h.dirLen; i++ {
			tp := *(*unsafe.Pointer)(unsafe.Add(h.dirPointer, uintptr(i)*unsafe.Sizeof(uintptr(0))))
			if tp == prev {
				aliased = true
				continue
			}
			if first, ok := seen[tp]; ok {
				t.Fatalf("size %d: table at directory entry %d reappears at entry %d after another table (dirLen=%d)", target, first, i, h.dirLen)
			}
			seen[tp] = i
			prev = tp
		}
		if got, want := mapSize[uint64, int](m), expectedSizeFromSwissInternals[uint64, int](m); got != want {
			t.Fatalf("size %d (dirLen=%d, tables=%d): mapSize=%d, want %d", target, h.dirLen, len(seen), got, want)
		}
	}
	if !aliased {
		t.Log("no directory entry aliased a table in this run: the dedup branch was not exercised")
	}
}

// TestMap_SizeSwiss_LargeMap_ZeroSizedValue: a large map[int]struct{} (zero-size values) is sized like the internals reference.
func TestMap_SizeSwiss_LargeMap_ZeroSizedValue(t *testing.T) {
	const N = 1 << 13

	m := make(map[int]struct{}, N)
	for i := range N {
		m[i] = struct{}{}
	}
	nudgeSwissGrowth[int, struct{}](m)

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil hmap")
	}
	requireDirLen(t, h, 1)

	got := mapSize[int, struct{}](m)
	want := expectedSizeFromSwissInternals[int, struct{}](m)
	if got != want {
		t.Fatalf("zero-val large-map (dirLen=%d): mapSize=%d, want %d (from internals)",
			h.dirLen, got, want)
	}
}

// TestMap_SizeSwiss_NilAndEmpty: a nil map has size 0 and an empty map is sized like the internals reference.
func TestMap_SizeSwiss_NilAndEmpty(t *testing.T) {
	t.Parallel()

	var mNil map[int]int
	if got := mapSize[int, int](mNil); got != 0 {
		t.Fatalf("nil mapSize = %d, want 0", got)
	}

	m := make(map[int]int)
	got := mapSize[int, int](m)
	want := expectedSizeFromSwissInternals[int, int](m)
	if got != want {
		t.Fatalf("empty mapSize = %d, want %d (from internals)", got, want)
	}
}

// TestMap_SizeSwiss_SmallMap_DirectGroup: a 4-entry map (one direct group, no directory) is sized like the internals reference.
func TestMap_SizeSwiss_SmallMap_DirectGroup(t *testing.T) {
	t.Parallel()

	m := make(map[int]int)
	for i := range 4 {
		m[i] = i
	}

	h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil hmap")
	}

	got := mapSize[int, int](m)
	want := expectedSizeFromSwissInternals[int, int](m)
	if got != want {
		t.Fatalf("small-map (dirLen=%d) mapSize=%d, want %d (from internals)", h.dirLen, got, want)
	}
}

// TestMap_SizeWithSwiss pins only alignUp arithmetic (it does not call mapSize): with
// the zero-size value padded by one byte, the slot of a map[int64]struct{} is 16 bytes.
func TestMap_SizeWithSwiss(t *testing.T) {
	t.Run("zero-sized value alignment", func(t *testing.T) {
		m := make(map[int64]struct{})
		m[0] = struct{}{}

		var k int64
		var v struct{}
		keySize, keyAlign := unsafe.Sizeof(k), unsafe.Alignof(k)
		valSize, valAlign := unsafe.Sizeof(v), unsafe.Alignof(v)

		valOffset := alignUp(keySize, valAlign)
		slotSize := valOffset + valSize
		if valSize == 0 {
			slotSize = alignUp(keySize+1, keyAlign)
		}

		expectedSlotSize := uintptr(16)

		if slotSize != expectedSlotSize {
			t.Errorf("expected slot size to be %d for map[int64]struct{}, but got %d", expectedSlotSize, slotSize)
		}
	})
}

// TestMap_SwissInternals_SanityChecks: the test-local mirrors (refHmap, refTableHdr, refGroupsRef) match the production structs in size and field offsets, and directory tables have power-of-two group counts.
func TestMap_SwissInternals_SanityChecks(t *testing.T) {
	t.Parallel()

	if got, want := unsafe.Sizeof(refHmap{}), unsafe.Sizeof(hmap{}); got != want {
		t.Errorf("refHmap size=%d != hmap size=%d — test and production mirrors diverged", got, want)
	}
	if got, want := unsafe.Sizeof(refTableHdr{}), unsafe.Sizeof(tableHdr{}); got != want {
		t.Errorf("refTableHdr size=%d != tableHdr size=%d — test and production mirrors diverged", got, want)
	}
	if got, want := unsafe.Sizeof(refGroupsRef{}), unsafe.Sizeof(groupsRef{}); got != want {
		t.Errorf("refGroupsRef size=%d != groupsRef size=%d — test and production mirrors diverged", got, want)
	}

	var refH refHmap
	var prodH hmap
	if offRef, offProd := unsafe.Offsetof(refH.dirPointer), unsafe.Offsetof(prodH.dirPointer); offRef != offProd {
		t.Errorf("refHmap.dirPointer offset=%d != hmap.dirPointer offset=%d — struct layout drifted", offRef, offProd)
	}
	if offRef, offProd := unsafe.Offsetof(refH.dirLen), unsafe.Offsetof(prodH.dirLen); offRef != offProd {
		t.Errorf("refHmap.dirLen offset=%d != hmap.dirLen offset=%d — struct layout drifted", offRef, offProd)
	}
	if offRef, offProd := unsafe.Offsetof(refH.clearSeq), unsafe.Offsetof(prodH.clearSeq); offRef != offProd {
		t.Errorf("refHmap.clearSeq offset=%d != hmap.clearSeq offset=%d — struct layout drifted", offRef, offProd)
	}

	var refT refTableHdr
	var prodT tableHdr
	if offRef, offProd := unsafe.Offsetof(refT.groups), unsafe.Offsetof(prodT.groups); offRef != offProd {
		t.Errorf("refTableHdr.groups offset=%d != tableHdr.groups offset=%d — struct layout drifted", offRef, offProd)
	}

	var refG refGroupsRef
	var prodG groupsRef
	if offRef, offProd := unsafe.Offsetof(refG.data), unsafe.Offsetof(prodG.data); offRef != offProd {
		t.Errorf("refGroupsRef.data offset=%d != groupsRef.data offset=%d — struct layout drifted", offRef, offProd)
	}
	if offRef, offProd := unsafe.Offsetof(refG.lengthMask), unsafe.Offsetof(prodG.lengthMask); offRef != offProd {
		t.Errorf("refGroupsRef.lengthMask offset=%d != groupsRef.lengthMask offset=%d — struct layout drifted", offRef, offProd)
	}

	var iZero int
	slotBytes := 2 * unsafe.Sizeof(iZero)
	const ctrlBytes, slotsPerGroup = uintptr(8), uintptr(8)
	wantGroupSize := ctrlBytes + slotsPerGroup*slotBytes
	if got := groupSizeFor[int, int](); got != wantGroupSize {
		t.Errorf("groupSizeFor[int,int]=%d, hand-derived want %d", got, wantGroupSize)
	}

	m := make(map[int]int)
	for i := range 1024 {
		m[i] = i
	}
	nudgeSwissGrowth[int, int](m)

	h := (*refHmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m)))
	if h == nil {
		t.Fatal("nil refHmap for populated map")
	}
	if h.dirLen <= 0 {
		t.Skipf("dirLen=%d: runtime used small-map layout, skipping power-of-2 check", h.dirLen)
	}

	const ptrSz = unsafe.Sizeof(uintptr(0))
	visited := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Add(h.dirPointer, uintptr(i)*ptrSz))
		if tp == nil {
			continue
		}
		if _, ok := visited[tp]; ok {
			continue
		}
		visited[tp] = struct{}{}

		tbl := (*refTableHdr)(tp)
		n := tbl.groups.lengthMask + 1
		if n == 0 || (n&(n-1)) != 0 {
			t.Errorf("table %p: lengthMask+1=%d is not a non-zero power of 2 (possible struct layout drift)", tp, n)
		}
	}
}

// TestSize_MapLowerBound: Map.Size of a one-entry map is at least the size of its mutex, read pointer, miss counter and dirty map.
func TestSize_MapLowerBound(t *testing.T) {
	gacheMap := new(Map[int, int])
	gacheMap.Store(1, 1)

	size := gacheMap.Size()
	if size <= 0 {
		t.Errorf("gacheMap.Size() should be positive, got %d", size)
	}

	expectedSize := unsafe.Sizeof(gacheMap.m.mu) +
		unsafe.Sizeof(gacheMap.m.read) +
		unsafe.Sizeof(gacheMap.m.misses) +
		mapSize(gacheMap.m.dirty)

	// Allow for some slack due to readOnlyShard struct and node sizes

	if size < expectedSize {
		t.Errorf("gacheMap.Size() %d is smaller than expected lower bound %d", size, expectedSize)
	}
}

// TestSize_MapGrowsAfterDelete: after a delete, Size is nonzero and still grows
// when another entry is stored.
func TestSize_MapGrowsAfterDelete(t *testing.T) {
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

// TestSize_MapDropsTombstonedEntries: tombstoned (deleted) nodes stay accounted for
// while they are still linked, and stop being counted once the next dirty-map
// rebuild and promotion physically drops them.
func TestSize_MapDropsTombstonedEntries(t *testing.T) {
	const n = 1000
	m := new(Map[int, int])
	for i := range n {
		m.Store(i, i)
	}
	m.Range(func(int, int) bool { return true }) // promote dirty to the read snapshot
	full := m.Size()

	for i := range n {
		m.Delete(i)
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("Len after deleting everything = %d, want 0", got)
	}
	tombstoned := m.Size()
	nodeSize := unsafe.Sizeof(valueOf[int, *int, struct{}, struct{}]{})
	if tombstoned > full || full-tombstoned > uintptr(n)*unsafe.Sizeof(int(0)) {
		// Only the boxed payloads leave the accounting; the tombstoned nodes are still linked.
		t.Fatalf("Size after Delete = %d, want within the payload bytes of %d", tombstoned, full)
	}

	m.Store(n, n)                                // amends the snapshot: the dirty rebuild skips every tombstoned node
	m.Range(func(int, int) bool { return true }) // promote the rebuilt dirty map
	after := m.Size()
	if want := uintptr(n/2) * nodeSize; tombstoned < after || tombstoned-after < want {
		t.Fatalf("Size after promotion = %d, want at least %d bytes below %d (tombstoned nodes dropped)", after, want, tombstoned)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
}

// TestSize_AllocFree asserts that Size of a large Map and of a populated Gache
// allocates nothing: mapSize walks the swiss directory without a dedup set.
func TestSize_AllocFree(t *testing.T) {
	// Not parallel: testing.AllocsPerRun needs a quiet process.
	m := new(Map[int, int])
	for i := range 1 << 17 {
		m.Store(i, i)
	}
	if h := (*hmap)(*(*unsafe.Pointer)(unsafe.Pointer(&m.m.dirty))); h == nil || h.dirLen < 2 {
		t.Fatalf("Map did not grow a multi-table directory: %+v", h)
	}
	if allocs := testing.AllocsPerRun(20, func() { _ = m.Size() }); allocs != 0 {
		t.Errorf("Map.Size allocs = %v, want 0", allocs)
	}

	g := New[int]().(*gache[int])
	for i := range 1 << 17 {
		g.Set(strconv.Itoa(i), i)
	}
	if allocs := testing.AllocsPerRun(5, func() { _ = g.Size() }); allocs != 0 {
		t.Errorf("Gache.Size allocs = %v, want 0", allocs)
	}
}

// TestMap_AlignUp_Basic: alignUp rounds 0, 1, 8 and 9 up to a multiple of 8.
func TestMap_AlignUp_Basic(t *testing.T) {
	t.Parallel()

	if got := alignUp(0, 8); got != 0 {
		t.Fatalf("alignUp(0,8) = %d, want 0", got)
	}
	if got := alignUp(1, 8); got != 8 {
		t.Fatalf("alignUp(1,8) = %d, want 8", got)
	}
	if got := alignUp(8, 8); got != 8 {
		t.Fatalf("alignUp(8,8) = %d, want 8", got)
	}
	if got := alignUp(9, 8); got != 16 {
		t.Fatalf("alignUp(9,8) = %d, want 16", got)
	}
}

// TestSize_TracksHeap checks Size of Map and gache against the live-heap delta of
// building them. Not parallel: the measurement needs a quiet heap.
func TestSize_TracksHeap(t *testing.T) {
	skipHeapMeasurement(t)
	const (
		n   = 100_000
		tol = 0.05
	)
	check := func(t *testing.T, real, est int64) {
		t.Helper()
		ratio := float64(est) / float64(real)
		t.Logf("Size=%d heap=%d Size/heap=%.3f", est, real, ratio)
		if ratio < 1-tol || ratio > 1+tol {
			t.Errorf("Size/heap = %.3f, want within %.0f%%", ratio, tol*100)
		}
	}
	mapBytes := func(m *Map[int, int]) uintptr { return m.Size() }
	arrSize := func(m *Map[int, [4]int64]) uintptr { return m.Size() }
	for _, state := range []string{"dirty", "read", "read+dirty"} {
		t.Run("Map[int,int]/"+state, func(t *testing.T) {
			real, est := retained(func() *Map[int, int] { return buildMap(n, state, func(i int) int { return i }) }, mapBytes)
			check(t, real, est)
		})
	}
	// [4]int64 boxes do not come from the tiny allocator, so deleting half the
	// entries frees half of them; an int box shares a 16-byte block with another
	// and stays charged by the heap while that one lives.
	for _, state := range []string{"dirty", "read", "read+dirty", "half deleted"} {
		t.Run("Map[int,[4]int64]/"+state, func(t *testing.T) {
			real, est := retained(func() *Map[int, [4]int64] {
				return buildMap(n, state, func(i int) [4]int64 { return [4]int64{int64(i)} })
			}, arrSize)
			check(t, real, est)
		})
	}

	// Key strings are excluded from gache's Size: they are built before the
	// baseline and stay alive.
	keys := make([]string, n)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	// 1000 keys leave most of the 4096 shards untouched, without a read snapshot.
	for _, keys := range [][]string{keys, keys[:1000]} {
		t.Run("Gache[int]/"+strconv.Itoa(len(keys)), func(t *testing.T) {
			real, est := retained(func() Gache[int] {
				g := New[int]()
				for i, k := range keys {
					g.Set(k, i)
				}
				return g
			}, func(g Gache[int]) uintptr { return g.Size() })
			check(t, real, est)
		})
	}
	t.Run("Gache[[4]int64]", func(t *testing.T) {
		real, est := retained(func() Gache[[4]int64] {
			g := New[[4]int64]()
			for i, k := range keys {
				g.Set(k, [4]int64{int64(i)})
			}
			return g
		}, func(g Gache[[4]int64]) uintptr { return g.Size() })
		check(t, real, est)
	})
	runtime.KeepAlive(keys)
}

// TestMap_RetainedBytesPerEntry bounds the heap a Map[int, int] retains per
// entry. Measured (Go 1.27.3, amd64, N=1e5, dirty only): 79.6 B/entry with the
// 40-byte node (48-byte size class); a 56-byte node (64-byte class) would retain
// 95.6. The bound of 88 sits between them.
func TestMap_RetainedBytesPerEntry(t *testing.T) {
	skipHeapMeasurement(t)
	const n = 100_000
	real, _ := retained(func() *Map[int, int] { return buildMap(n, "dirty", func(i int) int { return i }) },
		func(*Map[int, int]) uintptr { return 0 })
	if perEntry := float64(real) / n; perEntry > 88 {
		t.Errorf("Map[int, int] retains %.1f B/entry, want <= 88", perEntry)
	}
}

func TestSize_AllocSizeClasses(t *testing.T) {
	t.Parallel()
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("size classes are asserted for 64-bit platforms")
	}
	for _, tc := range []struct {
		size uintptr
		scan bool
		want uintptr
	}{
		{0, false, 0},
		{1, false, 1},   // 16 per tiny block
		{3, false, 3},   // 5 per block: 16/5
		{6, false, 8},   // 2 per block
		{8, false, 8},   // 2 per block
		{12, false, 16}, // 1 per block
		{8, true, 8},    // pointers: never tiny
		{12, true, 16},
		{16, false, 16},
		{40, true, 48},
		{56, true, 64},
		{136, true, 144},
		{512, true, 512},  // the malloc header starts above 512 bytes
		{513, false, 576}, // pointer-free: no header
		{513, true, 576},
		{1024, false, 1024},
		{1024, true, 1152}, // 1024 + 8-byte header
		{17408, true, 18432},
		{32760, true, 32768},
		{32761, true, 32768}, // large object: whole pages
		{32769, false, 40960},
		{196608, true, 196608},
	} {
		if got := allocSize(tc.size, tc.scan); got != tc.want {
			t.Errorf("allocSize(%d, %v) = %d, want %d", tc.size, tc.scan, got, tc.want)
		}
	}
	// Every small size against a linear scan of the class list.
	for size := uintptr(tinySize); size <= maxSmallSize-mallocHeaderSize; size++ {
		want := uintptr(0)
		for _, c := range sizeClasses {
			if uintptr(c) >= size {
				want = uintptr(c)
				break
			}
		}
		if got := allocSize(size, false); got != want {
			t.Fatalf("allocSize(%d, false) = %d, want %d", size, got, want)
		}
	}
	// Scan objects up to minSizeForMallocHeader have no header and skip the tiny
	// allocator: their class, including the sizes below tinySize.
	for size := uintptr(1); size <= minSizeForMallocHeader; size++ {
		want := uintptr(0)
		for _, c := range sizeClasses {
			if uintptr(c) >= size {
				want = uintptr(c)
				break
			}
		}
		if got := allocSize(size, true); got != want {
			t.Fatalf("allocSize(%d, true) = %d, want %d", size, got, want)
		}
	}
	// mapSizeScan charges a directory of 8..minSizeForMallocHeader bytes, a power
	// of two, unrounded.
	for size := uintptr(8); size <= minSizeForMallocHeader; size *= 2 {
		if got := allocSize(size, true); got != size {
			t.Errorf("allocSize(%d, true) = %d, want %d", size, got, size)
		}
	}
	// Map.Size skips typeHasPointers where scanMatters is false; that must not
	// change the charge of a size a pointerful type can have.
	for size := uintptr(wordSize); size <= 2*maxSmallSize; size += wordSize {
		if !scanMatters(size) && allocSize(size, true) != allocSize(size, false) {
			t.Errorf("scanMatters(%d) = false, but allocSize differs: %d (scan) vs %d", size, allocSize(size, true), allocSize(size, false))
		}
	}
}

// refHasPointers walks t like the runtime's PtrBytes computation, for
// TestSize_TypeHasPointers.
func refHasPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer,
		reflect.String, reflect.Slice, reflect.Interface:
		return true
	case reflect.Array:
		return t.Len() > 0 && refHasPointers(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if refHasPointers(field.Type) {
				return true
			}
		}
	}
	return false
}

func TestSize_TypeHasPointers(t *testing.T) {
	// Not parallel: testing.AllocsPerRun needs a quiet process.
	type noPtr struct {
		a int64
		b [3]int32
	}
	type withPtr struct {
		a int64
		p *int
	}
	check := func(name string, got bool, typ reflect.Type) {
		if want := refHasPointers(typ); got != want {
			t.Errorf("typeHasPointers[%s] = %v, want %v", name, got, want)
		}
	}
	check("int", typeHasPointers[int](), reflect.TypeFor[int]())
	check("[4]int64", typeHasPointers[[4]int64](), reflect.TypeFor[[4]int64]())
	check("noPtr", typeHasPointers[noPtr](), reflect.TypeFor[noPtr]())
	check("string", typeHasPointers[string](), reflect.TypeFor[string]())
	check("*int", typeHasPointers[*int](), reflect.TypeFor[*int]())
	check("withPtr", typeHasPointers[withPtr](), reflect.TypeFor[withPtr]())
	check("[0]*int", typeHasPointers[[0]*int](), reflect.TypeFor[[0]*int]())
	check("[]byte", typeHasPointers[[]byte](), reflect.TypeFor[[]byte]())
	check("any", typeHasPointers[any](), reflect.TypeFor[any]())
	if allocs := testing.AllocsPerRun(10, func() { _ = typeHasPointers[withPtr]() }); allocs != 0 {
		t.Errorf("typeHasPointers allocs = %v, want 0", allocs)
	}
}

// referenceNodes counts the distinct non-expunged nodes reachable from the read
// snapshot and the dirty map by a plain walk. It is the oracle for the node count
// shardMap.size() charges. It also checks the strict invariant that dirty never
// holds an expunged node.
func referenceNodes[K comparable, V any](t *testing.T, m *shardMap[K, V]) int {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[*value[K, V]]struct{})
	walk := func(heads map[uint64]*value[K, V], inDirty bool) {
		for _, head := range heads {
			for n := head; n != nil; n = loadNext(n) {
				if n.state.Load()&stMask == stExpunged {
					if inDirty {
						t.Fatalf("expunged node %v reachable from dirty", n.key)
					}
					continue
				}
				seen[n] = struct{}{}
			}
		}
	}
	walk(m.loadReadOnly().m, false)
	walk(m.dirty, true)
	return len(seen)
}

// sizeNodes recovers the node count size() charges from its result: everything
// else size() adds is recomputed here from the structures.
func sizeNodes[K comparable, V any](t *testing.T, m *shardMap[K, V]) int {
	t.Helper()
	got := m.size()
	m.mu.Lock()
	fixed := unsafe.Sizeof(*m) + mapSize(m.loadReadOnly().m) + mapSize(m.dirty)
	if m.read.Load() != nil {
		fixed += allocSize(unsafe.Sizeof(readOnlyShard[K, V]{}), true)
	}
	if m.l.Load() != nil {
		fixed += allocSize(unsafe.Sizeof(atomic.Int64{}), false)
	}
	m.mu.Unlock()
	rest := got - fixed
	nodeSize := allocSize(unsafe.Sizeof(value[K, V]{}), true)
	if rest%nodeSize != 0 {
		t.Fatalf("size() %d - fixed %d = %d is not a multiple of the %d byte node", got, fixed, rest, nodeSize)
	}
	return int(rest / nodeSize)
}

func checkSizeNodes[K comparable, V any](t *testing.T, m *shardMap[K, V], step string) {
	t.Helper()
	if want, got := referenceNodes(t, m), sizeNodes(t, m); want != got {
		t.Fatalf("%s: size() charges %d nodes, reference walk finds %d", step, got, want)
	}
}

// TestSize_ShardMapChainedNodesExact drives collision-heavy keys (13 keys per hash)
// through inserts, deletes, a dirty rebuild, promotions and Clear and checks after
// every step that size() charges exactly the nodes a walk finds.
func TestSize_ShardMapChainedNodesExact(t *testing.T) {
	t.Parallel()
	const keys, buckets = 130, 10
	hash := func(k int) uint64 { return uint64(k % buckets) }

	m := &shardMap[int, int]{}
	checkSizeNodes(t, m, "empty")
	for k := range keys {
		m.store(hash(k), k, k, 0)
		checkSizeNodes(t, m, fmt.Sprintf("insert %d", k))
	}
	m.readSnapshot() // promote
	checkSizeNodes(t, m, "promote")
	if got := sizeNodes(t, m); got != keys {
		t.Fatalf("after promote size() charges %d nodes, want %d", got, keys)
	}

	for k := 0; k < keys; k += 2 { // tombstones stay in read chains
		if _, ok := m.loadAndDelete(hash(k), k); !ok {
			t.Fatalf("delete %d: not loaded", k)
		}
		checkSizeNodes(t, m, fmt.Sprintf("tombstone %d", k))
	}
	m.store(hash(1000), 1000, 1, 0) // new key: dirty rebuild drops the dead nodes
	checkSizeNodes(t, m, "dirty rebuild")
	if got, want := sizeNodes(t, m), keys/2+1; got != want {
		t.Fatalf("after rebuild size() charges %d nodes, want %d", got, want)
	}

	for k := 1; k < keys; k += 4 { // shared nodes tombstoned while dirty exists
		m.loadAndDelete(hash(k), k)
		checkSizeNodes(t, m, fmt.Sprintf("shared delete %d", k))
	}
	for k := range 40 { // dirty-only inserts onto non-empty chains, then unlinks
		m.store(hash(k), 2000+k, k, 0)
		checkSizeNodes(t, m, fmt.Sprintf("dirty insert %d", k))
	}
	for k := range 40 {
		if _, ok := m.loadAndDelete(hash(k), 2000+k); !ok {
			t.Fatalf("delete dirty-only %d: not loaded", 2000+k)
		}
		checkSizeNodes(t, m, fmt.Sprintf("dirty unlink %d", k))
	}
	m.readSnapshot()
	checkSizeNodes(t, m, "second promote")

	m.clear()
	checkSizeNodes(t, m, "clear")
	if got := sizeNodes(t, m); got != 0 {
		t.Fatalf("after clear size() charges %d nodes, want 0", got)
	}
	for k := range keys {
		m.store(hash(k), k, k, 0)
	}
	checkSizeNodes(t, m, "insert after clear")

	r := &shardMap[int, int]{}
	r.initReserve(16)
	checkSizeNodes(t, r, "initReserve")
	for k := range keys {
		r.store(hash(k), k, k, 0)
	}
	checkSizeNodes(t, r, "insert after initReserve")
}

// TestSize_ShardMapChainedNodesRandom is the randomized variant: mixed stores,
// deletes, revivals, loads (which promote through missLocked), explicit promotions
// and Clear over a small collision-heavy key space.
func TestSize_ShardMapChainedNodesRandom(t *testing.T) {
	t.Parallel()
	for seed := range uint64(8) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
			hash := func(k int) uint64 { return uint64(k % 5) }
			m := &shardMap[int, int]{}
			if seed%2 == 1 {
				m.initReserve(8)
			}
			for i := range 4000 {
				k := rng.IntN(70)
				switch op := rng.IntN(100); {
				case op < 40:
					m.store(hash(k), k, i, 0)
				case op < 70:
					m.loadAndDelete(hash(k), k)
				case op < 85:
					m.loadValue(hash(k), k)
				case op < 90:
					m.loadOrStore(hash(k), k, i, 0)
				case op < 96:
					m.readSnapshot()
				case op < 97:
					m.clear()
				default:
					m.upsert(hash(k), k, i, 0, true)
				}
				checkSizeNodes(t, m, fmt.Sprintf("seed %d op %d", seed, i))
			}
		})
	}
}

// nodeBytes is the number of bytes Size() must at least charge for n
// entries of the cache's node type.
func nodeBytes(n int) uintptr {
	return uintptr(n) * unsafe.Sizeof(value[string, int]{})
}

// TestSize_GacheSameHashChains covers keys that collide on the 64-bit hash by
// design: only the first WithMaxKeyLength bytes are hashed, and keys longer than
// 64 bytes only by a 64-byte sample.
func TestSize_GacheSameHashChains(t *testing.T) {
	t.Parallel()
	const n = 10000

	t.Run("MaxKeyLength8", func(t *testing.T) {
		t.Parallel()
		g := New[int](WithMaxKeyLength[int](8))
		for i := range n {
			g.Set(fmt.Sprintf("prefix__%06d", i), i)
		}
		if got := g.Len(); got != n {
			t.Fatalf("Len = %d, want %d", got, n)
		}
		if got, lower := g.Size(), nodeBytes(n); got < lower {
			t.Fatalf("Size = %d is below the %d bytes of %d chained nodes", got, lower, n)
		}
	})

	t.Run("UnsampledBytes", func(t *testing.T) {
		t.Parallel()
		// 100-byte keys: bytes 16..24 and 41..74 are outside the four 16-byte
		// samples (0, q1=25, q3=75, last=84), so varying them cannot change the hash.
		g := New[int]()
		for i := range n {
			key := []byte(strings.Repeat("a", 100))
			copy(key[50:], fmt.Sprintf("%06d", i))
			g.Set(string(key), i)
		}
		if got := g.Len(); got != n {
			t.Fatalf("Len = %d, want %d", got, n)
		}
		if got, lower := g.Size(), nodeBytes(n); got < lower {
			t.Fatalf("Size = %d is below the %d bytes of %d chained nodes", got, lower, n)
		}
	})
}

// expectedMapSize recomputes Map.Size() from the shardMap structure: the struct
// itself, the length counter, the readOnlyShard header, both bucket maps, the
// nodes and the boxed value of every live entry, each heap object rounded by
// allocSize. Nodes are shared between the
// read snapshot and the dirty map and charged once, from the structure that holds
// all of them (dirty while it exists, else read). Their number is exact: one per
// bucket plus sm.chained, the nodes behind a bucket head: counting one per bucket
// would drop every chained node, and gache hashes only a key prefix or sample, so
// collisions are common.
func expectedMapSize[K comparable, V any](m *Map[K, V]) uintptr {
	sm := &m.m
	size := unsafe.Sizeof(*sm)
	if sm.l.Load() != nil {
		size += allocSize(unsafe.Sizeof(atomic.Int64{}), false)
	}
	ro := sm.read.Load()
	size += mapSize(sm.dirty)
	buckets := len(sm.dirty)
	if ro != nil {
		size += allocSize(unsafe.Sizeof(readOnlyShardOf[K, *V, struct{}, struct{}]{}), true)
		size += mapSize(ro.m)
		if sm.dirty == nil {
			buckets = len(ro.m)
		}
	}
	size += uintptr(buckets+sm.chained) * allocSize(unsafe.Sizeof(valueOf[K, *V, struct{}, struct{}]{}), true)
	return size + uintptr(m.Len())*allocSize(unsafe.Sizeof(*new(V)), typeHasPointers[V]())
}

// TestMap_SizeCorrectness: Size of a 1000-entry Map[int, int] equals expectedMapSize and is at least the node and value bytes of 1000 entries; Len is 1000.
func TestMap_SizeCorrectness(t *testing.T) {
	m := &Map[int, int]{}

	emptySize := m.Size()
	if emptySize <= 0 {
		t.Fatalf("expected empty size to be > 0, got %d", emptySize)
	}

	for i := range 1000 {
		m.Store(i, i)
	}

	expectedSize := expectedMapSize(m)
	actualSize := m.Size()
	if actualSize != expectedSize {
		t.Fatalf("expected size %d, got %d", expectedSize, actualSize)
	}
	if lower := 1000 * (unsafe.Sizeof(valueOf[int, *int, struct{}, struct{}]{}) + unsafe.Sizeof(int(0))); actualSize < lower {
		t.Fatalf("size %d is below the %d bytes of 1000 nodes and values", actualSize, lower)
	}
	if got := m.Len(); got != 1000 {
		t.Fatalf("Len = %d, want 1000", got)
	}
}

// TestMap_SizeStructCorrectness: Size of 1000 entries with a pointerful struct value equals expectedMapSize and is at least the node and value bytes.
func TestMap_SizeStructCorrectness(t *testing.T) {
	type ComplexStruct struct {
		D map[string]int
		B string
		C []byte
		A int64
	}

	m := &Map[string, ComplexStruct]{}

	for i := range 1000 {
		m.Store(fmt.Sprintf("key-%d", i), ComplexStruct{})
	}

	expectedSize := expectedMapSize(m)
	actualSize := m.Size()
	if actualSize != expectedSize {
		t.Fatalf("expected size %d, got %d", expectedSize, actualSize)
	}
	if lower := 1000 * (unsafe.Sizeof(valueOf[string, *ComplexStruct, struct{}, struct{}]{}) + unsafe.Sizeof(ComplexStruct{})); actualSize < lower {
		t.Fatalf("size %d is below the %d bytes of 1000 nodes and values", actualSize, lower)
	}
}

// TestMap_SizeChargesMallocHeader pins that Map.Size charges the malloc header of a
// boxed V that holds pointers and is larger than minSizeForMallocHeader: a 576-byte
// pointerful V falls into the 640-byte class with its header (the 576-byte class
// without), while a pointer-free V of the same size does not pay it.
func TestMap_SizeChargesMallocHeader(t *testing.T) {
	type withPointers [72]*int // 576 bytes, pointerful
	type withoutPointers [72]int

	if unsafe.Sizeof(withPointers{}) != unsafe.Sizeof(withoutPointers{}) || allocSize(unsafe.Sizeof(withPointers{}), true) == allocSize(unsafe.Sizeof(withPointers{}), false) {
		t.Skip("the chosen size no longer separates scan and noscan classes on this platform")
	}

	const n = 10
	var mp Map[int, withPointers]
	var mn Map[int, withoutPointers]
	for i := range n {
		mp.Store(i, withPointers{})
		mn.Store(i, withoutPointers{})
	}
	if got, want := mp.Size(), expectedMapSize(&mp); got != want {
		t.Fatalf("pointerful V: Size = %d, want %d", got, want)
	}
	if got, want := mn.Size(), expectedMapSize(&mn); got != want {
		t.Fatalf("pointer-free V: Size = %d, want %d", got, want)
	}
	// The two maps have identical node structures, so Size differs exactly by the box classes.
	wantDiff := uintptr(n) * (allocSize(unsafe.Sizeof(withPointers{}), true) - allocSize(unsafe.Sizeof(withoutPointers{}), false))
	if got := mp.Size() - mn.Size(); got != wantDiff {
		t.Fatalf("Size difference = %d, want %d (n boxes with vs without the malloc header)", got, wantDiff)
	}
}

// TestSize_GacheMonotonicAndConcurrent: Gache.Size grows with inserts, is unchanged
// by in-place overwrites, shrinks on Clear, and is never zero while writers run.
func TestSize_GacheMonotonicAndConcurrent(t *testing.T) {
	t.Run("Monotonicity_And_OverwriteIdempotency", func(t *testing.T) {
		gc := New[string]()
		baseSize := gc.Size()
		if baseSize == 0 {
			t.Fatalf("expected non-zero baseline cache size, got %d", baseSize)
		}

		// Insert 50 entries: size must strictly increase
		for i := range 50 {
			gc.Set(fmt.Sprintf("key_%d", i), "initial_value")
		}
		sizeAfterInserts := gc.Size()
		if sizeAfterInserts <= baseSize {
			t.Fatalf("expected size (%d) > baseline (%d) after 50 insertions", sizeAfterInserts, baseSize)
		}

		// Overwrite the same 50 entries in place: node count does not change, size should remain identical
		for i := range 50 {
			gc.Set(fmt.Sprintf("key_%d", i), "updated_value")
		}
		sizeAfterOverwrites := gc.Size()
		if sizeAfterOverwrites != sizeAfterInserts {
			t.Fatalf("expected size to remain identical after in-place overwrites: before=%d, after=%d",
				sizeAfterInserts, sizeAfterOverwrites)
		}

		// Clear() resets the cache: size should decrease back toward baseline
		gc.Clear()
		sizeAfterClear := gc.Size()
		if sizeAfterClear >= sizeAfterInserts {
			t.Fatalf("expected size after Clear (%d) < size with entries (%d)", sizeAfterClear, sizeAfterInserts)
		}
	})

	t.Run("Concurrent_Size_WithMutations", func(t *testing.T) {
		gc := New[int]()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Concurrent writers
		for w := range 4 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				i := 0
				for ctx.Err() == nil {
					gc.Set(fmt.Sprintf("k_%d_%d", id, i), i)
					if i%10 == 0 {
						gc.Delete(fmt.Sprintf("k_%d_%d", id, i-5))
					}
					i++
				}
			}(w)
		}

		// Concurrent Size() callers
		for range 2 {
			wg.Go(func() {
				for ctx.Err() == nil {
					sz := gc.Size()
					if sz == 0 {
						t.Errorf("unexpected zero size during concurrent mutation")
					}
					time.Sleep(1 * time.Millisecond)
				}
			})
		}

		wg.Wait()
	})
}
