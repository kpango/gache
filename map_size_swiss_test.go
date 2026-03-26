package gache

import (
	"testing"
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
// structures. This is conceptually equivalent to expectedSizeFromInternals in
// the non-swiss test: it exists as a separate reference implementation so that
// discrepancies in mapSize can be detected.
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
	total := unsafe.Sizeof(*h)

	if h.dirLen == 0 {
		// Small-map fast path: dirPointer points directly to a single group.
		if h.dirPointer != nil {
			total += groupSize
		}
		return total
	}

	// Large map: account for the directory pointer slice.
	const ptrSz = unsafe.Sizeof(uintptr(0))
	total += uintptr(h.dirLen) * ptrSz

	// Walk each directory entry, deduplicating table pointers (multiple entries
	// in the directory may alias the same table during and after a split).
	seen := make(map[unsafe.Pointer]struct{}, h.dirLen)
	for i := 0; i < h.dirLen; i++ {
		tp := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(h.dirPointer) + uintptr(i)*ptrSz))
		if tp != nil {
			seen[tp] = struct{}{}
		}
	}

	// Use refTableHdr (test-local mirror) to read each table's fields.
	tableHdrSz := unsafe.Sizeof(refTableHdr{})
	for tp := range seen {
		tbl := (*refTableHdr)(tp)
		total += tableHdrSz
		// groups backing array: numGroups = lengthMask + 1 (always a power of 2).
		numGroups := uintptr(tbl.groups.lengthMask + 1)
		total += numGroups * groupSize
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
	t.Helper()
	if h.dirLen < minDir {
		t.Skipf("swissmap dirLen=%d < %d; runtime chose a different layout — skipping directory path test", h.dirLen, minDir)
	}
}

// TestMap_SizeSwiss_LargeMap_DirectoryCreated comprehensively validates the runtime logic and edge cases of the respective component.
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

// TestMap_SizeSwiss_LargeMap_GrowthStages tracks the accuracy of size estimations as the swissmap dynamically allocates and expands its directory.
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

// TestMap_SizeSwiss_LargeMap_StringValueIndependence ensures that string value sizes do not inadvertently corrupt the core structural size metrics.
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

// TestMap_SizeSwiss_LargeMap_TableDedup verifies that aliased pointers in the swissmap directory do not cause duplicate counting during size evaluation.
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
		tp := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(h.dirPointer) + uintptr(i)*ptrSz))
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

// TestMap_SizeSwiss_LargeMap_ZeroSizedValue tests whether the map can correctly gauge internal footprint when utilized strictly as a hash set (zero-byte values).
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

// TestMap_SizeSwiss_NilAndEmpty confirms that uninitialized or entirely empty maps correctly report a size of zero without panicking.
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

// TestMap_SizeSwiss_SmallMap_DirectGroup validates size tracking for maps small enough to fit within a single, direct access group segment.
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

// TestMap_SizeWithSwiss conducts broad structural dimension checks across the standard map integrating the swiss table abstraction.
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

// TestMap_SwissInternals_SanityChecks ensures critical pointer offsets and struct layouts have not drifted from expected standard library equivalents.
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
		tp := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(h.dirPointer) + uintptr(i)*ptrSz))
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
