//go:build !arm64

package gache

import (
	"math/rand/v2"

	"github.com/zeebo/xxh3"
)

// xxh3Seed is drawn at init from math/rand/v2, independently of hashSeed, so that
// the hashes of 33B+ keys differ between processes and from the unseeded xxh3,
// whose collisions can be precomputed.
//
// A seeded xxh3 is not a keyed hash: it is weaker than the maphash that hashes the
// 2..32B keys against engineered collisions, so it does not give 33B+ keys the
// same resistance. The sampling caveat applies as well: a key longer than 64 bytes
// is hashed from a 64-byte sample (see [WithMaxKeyLength]), so keys of the same
// length that differ only outside the sample collide under any seed.
var xxh3Seed = rand.Uint64() //nolint:gosec,gochecknoglobals // G404: a seed for a non-cryptographic hash, not a secret key; process-wide like hashSeed

// hashMid hashes keys of 33..64 bytes.
func hashMid(key string) uint64 {
	return xxh3.HashStringSeed(key, xxh3Seed)
}

// hashSampled hashes the 64-byte sample built from keys longer than 64 bytes.
func hashSampled(buf *[64]byte) uint64 {
	return xxh3.HashSeed(buf[:], xxh3Seed)
}
