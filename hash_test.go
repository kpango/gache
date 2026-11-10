package gache

import (
	"fmt"
	"hash/maphash"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// getShardID follows the contract documented at WithMaxKeyLength.
func TestGetShardID_DocumentedHashing(t *testing.T) {
	t.Parallel()

	t.Run("0 and 1 byte keys are not hashed", func(t *testing.T) {
		t.Parallel()

		for _, kl := range []uint64{0, 256} {
			if got := getShardID("", kl); got != 0 {
				t.Errorf("getShardID(\"\", %d) = %d, want 0", kl, got)
			}
			if got := getShardID("x", kl); got != uint64('x') {
				t.Errorf("getShardID(\"x\", %d) = %d, want %d", kl, got, uint64('x'))
			}
		}
	})

	t.Run("2 to 32 bytes use maphash", func(t *testing.T) {
		t.Parallel()

		for _, n := range []int{2, 17, 32} {
			key := strings.Repeat("a", n-1) + "b"
			if got, want := getShardID(key, 0), maphash.String(hashSeed, key); got != want {
				t.Errorf("len %d: getShardID = %#x, want maphash %#x", n, got, want)
			}
		}
	})

	t.Run("33 to 64 bytes use hashMid", func(t *testing.T) {
		t.Parallel()

		for _, n := range []int{33, 48, 64} {
			key := strings.Repeat("a", n-1) + "b"
			if got, want := getShardID(key, 0), hashMid(key); got != want {
				t.Errorf("len %d: getShardID = %#x, want hashMid %#x", n, got, want)
			}
		}
	})

	// A 100-byte key is sampled in four 16-byte windows: bytes 0..15, 25..40,
	// 75..90 and 84..99 (a quarter, three quarters of the length, and the end).
	t.Run("keys over 64 bytes are sampled", func(t *testing.T) {
		t.Parallel()

		base := []byte(strings.Repeat("a", 100))
		with := func(i int) string {
			k := append([]byte(nil), base...)
			k[i] = 'z'
			return string(k)
		}
		ref := getShardID(string(base), 0)
		for _, i := range []int{16, 24, 41, 50, 74} {
			if got := getShardID(with(i), 0); got != ref {
				t.Errorf("a difference at byte %d, outside every window, changed the hash", i)
			}
		}
		for _, i := range []int{0, 15, 25, 40, 75, 90, 99} {
			if got := getShardID(with(i), 0); got == ref {
				t.Errorf("a difference at byte %d, inside a window, did not change the hash", i)
			}
		}
		if got := getShardID(string(base)+"a", 0); got == ref {
			t.Error("a longer key of the same bytes hashed like the shorter one: the length is not mixed in")
		}
	})

	// 0 is no prefix limit; it does not make the hash cover the whole key.
	t.Run("a limit hashes the prefix only", func(t *testing.T) {
		t.Parallel()

		a := strings.Repeat("a", 299) + "x"
		b := strings.Repeat("a", 299) + "y"
		if getShardID(a, 0) == getShardID(b, 0) {
			t.Error("kl 0: keys that differ in the last window collided")
		}
		if getShardID(a, 256) != getShardID(b, 256) {
			t.Error("kl 256: keys that share their first 256 bytes did not collide")
		}
	})

	// A key of kl bytes is hashed whole, one of kl+1 bytes from its first kl bytes
	// only: the limit applies from the first byte past it, at every hash width.
	t.Run("the limit applies from the byte past it", func(t *testing.T) {
		t.Parallel()

		for _, kl := range []uint64{1, 2, 8, 32, 33, 64, 65, 256} {
			prefix := strings.Repeat("p", int(kl))
			over, other := prefix+"x", prefix+"y"
			if got, want := getShardID(over, kl), getShardID(prefix, 0); got != want {
				t.Errorf("kl %d: a key of %d bytes hashes to %#x, want the hash of its first %d bytes %#x", kl, kl+1, got, kl, want)
			}
			if getShardID(over, kl) != getShardID(other, kl) {
				t.Errorf("kl %d: keys of %d bytes sharing their first %d did not collide", kl, kl+1, kl)
			}
		}
	})
}

// TestGetShardID_MaxKeyLength: with a limit kl, keys that share their first kl bytes
// get the hash of that prefix (so they collide whatever follows), a key not longer
// than kl is unaffected by the limit, and the hash is deterministic.
func TestGetShardID_MaxKeyLength(t *testing.T) {
	t.Parallel()

	long64 := strings.Repeat("a", 64)
	tests := []struct {
		name string
		kl   uint64
		a, b string // differ after their first kl bytes
	}{
		{"kl=1 first byte only", 1, "abc", "axyz"},
		{"kl=2", 2, long64, long64 + "different-suffix"},
		{"kl=16", 16, long64, long64 + "different-suffix"},
		{"kl=16 prefix", 16, strings.Repeat("p", 16) + "AAAAAA", strings.Repeat("p", 16) + "BBBBBB"},
		{"kl=32", 32, long64, long64 + "different-suffix"},
		{"kl=33 (hashMid)", 33, strings.Repeat("c", 33) + "suffix1", strings.Repeat("c", 33) + "suffix2"},
		{"kl=50", 50, strings.Repeat("q", 50) + "SUFFIX1", strings.Repeat("q", 50) + "SUFFIX2"},
		{"kl=64", 64, strings.Repeat("c", 64) + "suffix1", strings.Repeat("c", 64) + "suffix2"},
		{"kl=256 (sampled)", 256, strings.Repeat("c", 256) + "suffix1", strings.Repeat("c", 256) + "suffix2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ida, idb := getShardID(tt.a, tt.kl), getShardID(tt.b, tt.kl)
			if ida != idb {
				t.Errorf("keys sharing their first %d bytes hash to %#x and %#x, want equal", tt.kl, ida, idb)
			}
			if want := getShardID(tt.a[:tt.kl], 0); ida != want {
				t.Errorf("getShardID(key, %d) = %#x, want the hash of the first %d bytes %#x", tt.kl, ida, tt.kl, want)
			}
			if again := getShardID(tt.a, tt.kl); again != ida {
				t.Errorf("getShardID is not deterministic: %#x then %#x", ida, again)
			}
		})
	}

	t.Run("a key not longer than the limit is hashed whole", func(t *testing.T) {
		t.Parallel()

		if a, b := getShardID("hi", 100), getShardID("hi", 2); a != b {
			t.Errorf("getShardID(\"hi\", 100) = %#x, getShardID(\"hi\", 2) = %#x, want equal", a, b)
		}
		if got := getShardID("x", 50); got != uint64('x') {
			t.Errorf("getShardID(\"x\", 50) = %d, want %d", got, uint64('x'))
		}
	})

	t.Run("no limit is deterministic for every length class", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"a", "ab", strings.Repeat("x", 32), strings.Repeat("y", 33), strings.Repeat("z", 256)} {
			if a, b := getShardID(key, 0), getShardID(key, 0); a != b {
				t.Errorf("getShardID(len %d, 0) is not deterministic: %#x then %#x", len(key), a, b)
			}
		}
	})

	t.Run("no limit spreads keys over the shards", func(t *testing.T) {
		t.Parallel()

		used := map[uint64]struct{}{}
		for i := range 4 * slen { // expected coverage is slen*(1-e^-4), about 98%
			used[getShardID(fmt.Sprintf("key-%d", i), 0)&mask] = struct{}{}
		}
		if len(used) < slen*9/10 {
			t.Errorf("%d keys reached %d of %d shards, want at least %d", 4*slen, len(used), slen, slen*9/10)
		}
	})
}

// TestGetShardID_SmallKeyFastPaths pins the keys that are not hashed (empty: 0,
// one byte: that byte, for every byte value and limit) and that the 32 and 33 byte
// keys, which take different hash functions, hash to distinct nonzero values.
func TestGetShardID_SmallKeyFastPaths(t *testing.T) {
	// Length 0 must return 0
	if id := getShardID("", 256); id != 0 {
		t.Errorf("getShardID(\"\", 256) = %d, expected 0", id)
	}
	if id := getShardID("", 0); id != 0 {
		t.Errorf("getShardID(\"\", 0) = %d, expected 0", id)
	}

	// Length 1 must return uint64(key[0])
	for b := range 256 {
		k := string([]byte{byte(b)})
		expected := uint64(byte(b))
		if id := getShardID(k, 256); id != expected {
			t.Errorf("getShardID(%q, 256) = %d, expected %d", k, id, expected)
		}
		if id := getShardID(k, 1); id != expected {
			t.Errorf("getShardID(%q, 1) = %d, expected %d", k, id, expected)
		}
		if id := getShardID(k, 0); id != expected {
			t.Errorf("getShardID(%q, 0) = %d, expected %d", k, id, expected)
		}
	}

	// Length > 1 with maxKeyLength = 1 truncation must match uint64(key[0])
	if id := getShardID("hello", 1); id != uint64('h') {
		t.Errorf("getShardID(\"hello\", 1) = %d, expected %d", id, uint64('h'))
	}

	// Length 2..32 vs > 32
	k32 := "12345678901234567890123456789012"
	k33 := "123456789012345678901234567890123"
	id32 := getShardID(k32, 256)
	id33 := getShardID(k33, 256)
	if id32 == 0 || id33 == 0 {
		t.Errorf("unexpected zero hash for 32/33 byte keys")
	}
	if id32 == id33 {
		t.Errorf("hash collision between 32 and 33 byte keys")
	}
}

// TestGetShardID_KeyLengthsAndHashing stores, reads, overwrites and deletes keys of
// each length class through a cache: the empty key, 1-byte keys, the 32 and 33 byte
// keys (maphash and hashMid), keys that share a WithMaxKeyLength prefix (one
// collision chain) and 301-byte keys under no limit.
func TestGetShardID_KeyLengthsAndHashing(t *testing.T) {
	t.Run("EmptyKey_Len0", func(t *testing.T) {
		gc := New[string]()
		h := getShardID("", 256)
		if h != 0 {
			t.Fatalf("expected getShardID for empty key to be 0, got %d", h)
		}

		gc.Set("", "empty_val")
		if v, ok := gc.Get(""); !ok || v != "empty_val" {
			t.Fatalf("failed to retrieve empty key: val=%s, ok=%v", v, ok)
		}

		if gc.Len() != 1 {
			t.Fatalf("expected Len=1, got %d", gc.Len())
		}

		// Overwrite empty key
		gc.Set("", "empty_val_updated")
		if v, ok := gc.Get(""); !ok || v != "empty_val_updated" {
			t.Fatalf("failed to retrieve updated empty key: val=%s, ok=%v", v, ok)
		}
		if gc.Len() != 1 {
			t.Fatalf("expected Len=1 after overwrite, got %d", gc.Len())
		}

		// Delete empty key
		v, deleted := gc.Delete("")
		if !deleted || v != "empty_val_updated" {
			t.Fatalf("failed to delete empty key: val=%s, deleted=%v", v, deleted)
		}
		if gc.Len() != 0 {
			t.Fatalf("expected Len=0 after delete, got %d", gc.Len())
		}
	})

	t.Run("OneByteKeys_DirectByteFastPath", func(t *testing.T) {
		gc := New[int]()
		// Test various 1-byte keys including ASCII, null byte, high-bit bytes
		testBytes := []byte{0x00, 0x01, 'a', 'Z', '0', 0x7F, 0x80, 0xFE, 0xFF}
		for _, b := range testBytes {
			k := string([]byte{b})
			expectedH := uint64(b)
			actualH := getShardID(k, 256)
			if actualH != expectedH {
				t.Fatalf("expected hash for 1-byte key %q to be %d, got %d", k, expectedH, actualH)
			}
			gc.Set(k, int(b))
		}

		if gc.Len() != len(testBytes) {
			t.Fatalf("expected Len=%d, got %d", len(testBytes), gc.Len())
		}

		for _, b := range testBytes {
			k := string([]byte{b})
			v, ok := gc.Get(k)
			if !ok || v != int(b) {
				t.Fatalf("failed to retrieve 1-byte key %q: val=%d, ok=%v", k, v, ok)
			}
		}
	})

	t.Run("HashBoundary_32BytesVs33Bytes", func(t *testing.T) {
		gc := New[string]()
		// Exactly 32 bytes (maphash threshold: lk <= 32)
		k32 := strings.Repeat("A", 32)
		// Exactly 33 bytes (hashMid: xxh3, or maphash on arm64)
		k33 := strings.Repeat("A", 33)

		h32 := getShardID(k32, 256)
		h33 := getShardID(k33, 256)

		if h32 == h33 {
			t.Fatalf("hash collision between 32-byte key and 33-byte key")
		}

		gc.Set(k32, "val32")
		gc.Set(k33, "val33")

		if v, ok := gc.Get(k32); !ok || v != "val32" {
			t.Fatalf("failed to retrieve 32-byte key")
		}
		if v, ok := gc.Get(k33); !ok || v != "val33" {
			t.Fatalf("failed to retrieve 33-byte key")
		}
	})

	t.Run("MaxKeyLength_CollisionChainResolution", func(t *testing.T) {
		// When WithMaxKeyLength is 16, keys sharing the first 16 bytes produce the EXACT same
		// shard hash and shard ID, forcing them into the same collision bucket chain.
		const maxLen = 16
		gc := New[string](WithMaxKeyLength[string](maxLen))

		const prefix = "common_prefix_16"
		const keyCount = 20

		keys := make([]string, keyCount)
		for i := range keyCount {
			// Keys are 267 bytes long, differing only from byte offset 256 on
			keys[i] = prefix + strings.Repeat("x", 240) + fmt.Sprintf("_suffix_%03d", i)
			gc.Set(keys[i], fmt.Sprintf("val_%d", i))
		}

		// Verify all keys have identical shard IDs
		h0 := getShardID(keys[0], maxLen)
		for i := 1; i < keyCount; i++ {
			hi := getShardID(keys[i], maxLen)
			if hi != h0 {
				t.Fatalf("expected keys sharing first 16 bytes to have same hash %d, got %d for key %d", h0, hi, i)
			}
		}

		// All 20 items must be correctly stored in the collision chain
		if gc.Len() != keyCount {
			t.Fatalf("expected Len=%d, got %d", keyCount, gc.Len())
		}

		// Verify retrieval of every individual item
		for i := range keyCount {
			expectedVal := fmt.Sprintf("val_%d", i)
			if v, ok := gc.Get(keys[i]); !ok || v != expectedVal {
				t.Fatalf("failed to retrieve colliding key %s: got %s, ok=%v", keys[i], v, ok)
			}
		}

		// Delete alternate keys to verify middle-of-chain unlinking
		for i := 0; i < keyCount; i += 2 {
			_, deleted := gc.Delete(keys[i])
			if !deleted {
				t.Fatalf("failed to delete colliding key %d", i)
			}
		}

		if gc.Len() != keyCount/2 {
			t.Fatalf("expected Len=%d after deleting half, got %d", keyCount/2, gc.Len())
		}

		// Verify surviving keys remain intact
		for i := 1; i < keyCount; i += 2 {
			expectedVal := fmt.Sprintf("val_%d", i)
			if v, ok := gc.Get(keys[i]); !ok || v != expectedVal {
				t.Fatalf("surviving colliding key %d missing or corrupted", i)
			}
		}
	})

	t.Run("MaxKeyLength_0_NoPrefixLimit", func(t *testing.T) {
		// WithMaxKeyLength(0) sets no prefix limit; a key over 64 bytes is still
		// sampled (see TestGetShardID_DocumentedHashing), and these keys differ in
		// the last sampled window.
		gc := New[int](WithMaxKeyLength[int](0))

		k1 := strings.Repeat("A", 300) + "1"
		k2 := strings.Repeat("A", 300) + "2"

		h1 := getShardID(k1, 0)
		h2 := getShardID(k2, 0)

		if h1 == h2 {
			t.Fatalf("expected distinct hashes when WithMaxKeyLength(0), but both got %d", h1)
		}

		gc.Set(k1, 1)
		gc.Set(k2, 2)

		if v, ok := gc.Get(k1); !ok || v != 1 {
			t.Fatalf("failed to retrieve k1")
		}
		if v, ok := gc.Get(k2); !ok || v != 2 {
			t.Fatalf("failed to retrieve k2")
		}
	})
}

// getShardID is on every operation's path: hashing a key of any length class
// (empty, 1, 32, 33, 64, 100 bytes) with no limit and with limits that trim the key
// allocates nothing. Not parallel: testing.AllocsPerRun needs a quiet process.
func TestGetShardID_NoAllocs(t *testing.T) {
	for _, key := range []string{
		"",
		"a",
		strings.Repeat("k", 32),
		strings.Repeat("k", 33),
		strings.Repeat("k", 64),
		strings.Repeat("k", 100),
	} {
		for _, kl := range []uint64{0, 16, 256} {
			if n := testing.AllocsPerRun(1000, func() { _ = getShardID(key, kl) }); n != 0 {
				t.Errorf("getShardID(len %d, %d) allocated %v allocs/op, want 0", len(key), kl, n)
			}
		}
	}
}

var hashSink uint64

// BenchmarkGetShardID measures getShardID alone, per key length, with
// maxKeyLength 0 so keys longer than 64 bytes take the sampling path.
//
//	1       trivial path
//	8..32   maphash.String
//	33..64  hashMid
//	65+     hashLongKey sampling + hashSampled
func BenchmarkGetShardID(b *testing.B) {
	const ring = 256 // power of two; distinct keys per length so the loop does not hash a single hot key
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{1, 8, 16, 32, 33, 48, 64, 65, 128, 256, 1024, 4096} {
		keys := make([]string, ring)
		for i := range keys {
			buf := make([]byte, n)
			for j := range buf {
				buf[j] = byte('a' + rng.IntN(26))
			}
			keys[i] = string(buf)
		}
		b.Run("len"+strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			var h uint64
			var i int
			for b.Loop() {
				h += getShardID(keys[i&(ring-1)], 0)
				i++
			}
			hashSink = h
		})
	}
}
