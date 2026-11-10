//go:build !arm64

package gache

import (
	"hash/maphash"
	"strings"
	"testing"

	"github.com/zeebo/xxh3"
)

func TestXXH3SeedChangesHash(t *testing.T) {
	t.Parallel()

	const seedA, seedB uint64 = 0x9e3779b97f4a7c15, 0xc2b2ae3d27d4eb4f
	for _, n := range []int{48, 64, 100} {
		key := strings.Repeat("k", n-1) + "x"
		a := xxh3.HashStringSeed(key, seedA)
		b := xxh3.HashStringSeed(key, seedB)
		if a == b {
			t.Errorf("len %d: different seeds gave the same hash %#x", n, a)
		}
		if a == xxh3.HashString(key) {
			t.Errorf("len %d: seeded hash equals unseeded hash %#x", n, a)
		}
	}

	var bufA [64]byte
	for i := range bufA {
		bufA[i] = byte(i)
	}
	if xxh3.HashSeed(bufA[:], seedA) == xxh3.HashSeed(bufA[:], seedB) {
		t.Error("sampled buffer: different seeds gave the same hash")
	}
}

// The xxh3 seed is drawn on its own at init: a seed derived from hashSeed with a
// fixed label would be a function of the maphash seed.
func TestXXH3SeedIndependentOfHashSeed(t *testing.T) {
	t.Parallel()

	if xxh3Seed == maphash.String(hashSeed, "gache/xxh3") {
		t.Fatal("xxh3Seed equals the maphash of a fixed label under hashSeed: it is derived, not drawn")
	}
}

func TestHashMidSeeded(t *testing.T) {
	t.Parallel()

	if xxh3Seed == 0 {
		t.Fatal("xxh3Seed is zero, hashing would be unseeded")
	}
	key := strings.Repeat("m", 48)
	want := xxh3.HashStringSeed(key, xxh3Seed)
	for range 3 {
		if got := hashMid(key); got != want {
			t.Fatalf("hashMid = %#x, want %#x", got, want)
		}
	}
	var buf [64]byte
	copy(buf[:], strings.Repeat("s", 64))
	if got, want := hashSampled(&buf), xxh3.HashSeed(buf[:], xxh3Seed); got != want {
		t.Fatalf("hashSampled = %#x, want %#x", got, want)
	}
	if hashMid(key) == xxh3.HashString(key) {
		t.Fatal("hashMid still equals the unseeded xxh3 hash")
	}
}

func TestHashSeededNoAllocs(t *testing.T) {
	key := strings.Repeat("a", 48)
	var buf [64]byte
	var sink uint64
	if n := testing.AllocsPerRun(1000, func() { sink += hashMid(key) }); n != 0 {
		t.Errorf("hashMid allocs = %v, want 0", n)
	}
	if n := testing.AllocsPerRun(1000, func() { sink += hashSampled(&buf) }); n != 0 {
		t.Errorf("hashSampled allocs = %v, want 0", n)
	}
	long := strings.Repeat("z", 100)
	if n := testing.AllocsPerRun(1000, func() { sink += hashLongKey(long, uint64(len(long))) }); n != 0 {
		t.Errorf("hashLongKey allocs = %v, want 0", n)
	}
	if sink == 0 {
		t.Log("sink is zero")
	}
}
