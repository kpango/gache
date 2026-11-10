//go:build arm64

package gache

import "hash/maphash"

// On arm64 the runtime's memhash (maphash) uses the ARMv8 AES instructions when
// the CPU has them (cpu.ARM64.HasAES) and the runtime's portable fallback
// otherwise, while xxh3 is scalar for inputs <= 240 bytes. Measured with
// BenchmarkGetShardID, GitHub Actions run 42503701824, ubuntu-24.04-arm
// (Neoverse N2) vs ubuntu-latest (EPYC 9V74), count=6 interleaved, hosted-runner
// noise floor ~8%: on arm64 maphash takes 13% less time per op for 33..64B keys
// and 17..26% less for sampled long keys; on amd64 the same switch takes 8..74%
// more, so the other architectures keep xxh3 (hash_other.go).

// hashMid hashes keys of 33..64 bytes.
func hashMid(key string) uint64 {
	return maphash.String(hashSeed, key)
}

// hashSampled hashes the 64-byte sample built from keys longer than 64 bytes.
func hashSampled(buf *[64]byte) uint64 {
	return maphash.Bytes(hashSeed, buf[:])
}
