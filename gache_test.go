package gache

import (
	"strings"
	"testing"
)

// TestGetShardID_MaxKeyLengthZero tests getShardID when maxKeyLength (kl) is 0,
// meaning the full key is used for hashing.
func TestGetShardID_MaxKeyLengthZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
	}{
		{name: "single char key", key: "a"},
		{name: "two char key", key: "ab"},
		{name: "32 char key", key: strings.Repeat("x", 32)},
		{name: "33 char key (uses xxh3)", key: strings.Repeat("y", 33)},
		{name: "long key (uses xxh3)", key: strings.Repeat("z", 256)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id := getShardID(tt.key, 0)
			if id > mask {
				t.Errorf("getShardID(%q, 0) = %d, want <= %d (mask)", tt.key, id, mask)
			}
			// Result must be deterministic within the same process.
			id2 := getShardID(tt.key, 0)
			if id != id2 {
				t.Errorf("getShardID(%q, 0) is not deterministic: got %d then %d", tt.key, id, id2)
			}
		})
	}
}

// TestGetShardID_MaxKeyLengthOne tests getShardID when kl == 1,
// which should use only the first byte of the key.
func TestGetShardID_MaxKeyLengthOne(t *testing.T) {
	t.Parallel()

	// When kl=1, the shard ID is determined solely by the first byte.
	idA1 := getShardID("abc", 1)
	idA2 := getShardID("axyz", 1)
	if idA1 != idA2 {
		t.Errorf("keys with the same first byte should map to the same shard with kl=1: got %d and %d", idA1, idA2)
	}

	// Keys starting with different bytes should (in general) differ; verify bounds only.
	idB := getShardID("bcd", 1)
	if idB > mask {
		t.Errorf("getShardID(%q, 1) = %d, want <= %d (mask)", "bcd", idB, mask)
	}
	// Manually verify: first byte of "abc" is 'a' == 97; 97 & mask == 97.
	want := uint64('a') & mask
	if idA1 != want {
		t.Errorf("getShardID(%q, 1) = %d, want %d", "abc", idA1, want)
	}
}

// TestGetShardID_MaxKeyLengthBetween1And32 tests getShardID when kl is 2..32.
func TestGetShardID_MaxKeyLengthBetween1And32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kl   uint64
	}{
		{name: "kl=2", kl: 2},
		{name: "kl=16", kl: 16},
		{name: "kl=32", kl: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			longKey := strings.Repeat("a", 64)
			shortKey := longKey[:tt.kl/2+1] // shorter than kl

			idLong := getShardID(longKey, tt.kl)
			if idLong > mask {
				t.Errorf("getShardID(longKey, %d) = %d, want <= %d (mask)", tt.kl, idLong, mask)
			}

			idShort := getShardID(shortKey, tt.kl)
			if idShort > mask {
				t.Errorf("getShardID(shortKey, %d) = %d, want <= %d (mask)", tt.kl, idShort, mask)
			}

			// Long keys truncated to kl bytes: two keys with the same first kl bytes
			// must hash to the same shard.
			longKey2 := strings.Repeat("a", 64) + "different-suffix"
			idLong2 := getShardID(longKey2, tt.kl)
			if idLong != idLong2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, idLong, idLong2)
			}

			// Determinism
			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGetShardID_MaxKeyLengthOver32 tests getShardID when kl > 32 (uses xxh3 path).
func TestGetShardID_MaxKeyLengthOver32(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kl   uint64
	}{
		{name: "kl=33", kl: 33},
		{name: "kl=64", kl: 64},
		{name: "kl=256", kl: 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			longKey := strings.Repeat("b", int(tt.kl)*2)
			shortKey := strings.Repeat("b", int(tt.kl)/2)

			idLong := getShardID(longKey, tt.kl)
			if idLong > mask {
				t.Errorf("getShardID(longKey, %d) = %d, want <= %d (mask)", tt.kl, idLong, mask)
			}

			idShort := getShardID(shortKey, tt.kl)
			if idShort > mask {
				t.Errorf("getShardID(shortKey, %d) = %d, want <= %d (mask)", tt.kl, idShort, mask)
			}

			// Two keys with the same prefix of length kl must hash to the same shard.
			key1 := strings.Repeat("c", int(tt.kl)) + "suffix1"
			key2 := strings.Repeat("c", int(tt.kl)) + "suffix2"
			id1 := getShardID(key1, tt.kl)
			id2 := getShardID(key2, tt.kl)
			if id1 != id2 {
				t.Errorf("keys with identical first %d bytes must map to the same shard: got %d and %d", tt.kl, id1, id2)
			}

			// Determinism
			if idLong != getShardID(longKey, tt.kl) {
				t.Errorf("getShardID is not deterministic for kl=%d", tt.kl)
			}
		})
	}
}

// TestGetShardID_KeyShorterThanMaxKeyLength tests that when the key is shorter
// than kl, the full key is used.
func TestGetShardID_KeyShorterThanMaxKeyLength(t *testing.T) {
	t.Parallel()

	// "hi" has length 2; kl=100 → effective kl = min(2,100) = 2.
	// Should equal getShardID("hi", 2).
	key := "hi"
	id1 := getShardID(key, 100)
	id2 := getShardID(key, 2)
	if id1 != id2 {
		t.Errorf("short key: getShardID(%q, 100)=%d should equal getShardID(%q, 2)=%d", key, id1, key, id2)
	}

	// Single-byte key with large kl: effective kl=1, must equal first-byte path.
	singleKey := "x"
	idSingle := getShardID(singleKey, 50)
	want := uint64(singleKey[0]) & mask
	if idSingle != want {
		t.Errorf("getShardID(%q, 50) = %d, want %d", singleKey, idSingle, want)
	}
}

// TestGetShardID_ResultInRange verifies the result is always within [0, mask].
func TestGetShardID_ResultInRange(t *testing.T) {
	t.Parallel()

	type tc struct {
		key string
		kl  uint64
	}
	cases := []tc{
		{"a", 0},
		{"a", 1},
		{"hello", 0},
		{"hello", 3},
		{strings.Repeat("k", 32), 0},
		{strings.Repeat("k", 32), 32},
		{strings.Repeat("k", 33), 0},
		{strings.Repeat("k", 33), 33},
		{strings.Repeat("k", 256), 64},
		{strings.Repeat("k", 256), 256},
	}

	for _, c := range cases {
		id := getShardID(c.key, c.kl)
		if id > mask {
			t.Errorf("getShardID(%q, %d) = %d exceeds mask %d", c.key, c.kl, id, mask)
		}
	}
}

// TestGetShardID_PrefixIsolation verifies that two keys differing only in bytes
// beyond position kl are assigned the same shard.
func TestGetShardID_PrefixIsolation(t *testing.T) {
	t.Parallel()

	prefix := strings.Repeat("p", 16)
	key1 := prefix + "AAAAAA"
	key2 := prefix + "BBBBBB"

	// With kl=16, only the first 16 bytes matter.
	id1 := getShardID(key1, 16)
	id2 := getShardID(key2, 16)
	if id1 != id2 {
		t.Errorf("prefix isolation failed: getShardID(%q, 16)=%d != getShardID(%q, 16)=%d", key1, id1, key2, id2)
	}

	// With kl > 32 (xxh3 path), same check.
	longPrefix := strings.Repeat("q", 50)
	key3 := longPrefix + "SUFFIX1"
	key4 := longPrefix + "SUFFIX2"
	id3 := getShardID(key3, 50)
	id4 := getShardID(key4, 50)
	if id3 != id4 {
		t.Errorf("prefix isolation (xxh3) failed: getShardID(%q, 50)=%d != getShardID(%q, 50)=%d", key3, id3, key4, id4)
	}
}
