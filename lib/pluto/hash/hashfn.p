// Package hash provides hash functions and a hash map and set that take an
// explicit allocator.
//
// The hash functions are not cryptographic. String, Bytes and Uint64 are
// fast mixing functions in the style of wyhash; FNV1a and FNV1aString are
// the classic 64-bit FNV-1a, for formats that specify it.
//
// Map and Set are open-addressing tables with linear probing, the tables of
// the built-in map[K]V held as owners. They take a seed but use a fixed one
// by default, so that they iterate in the same order in every run: do not
// use them to index attacker-controlled keys without a secret seed
// (InitSeed) or a keyed hash function. map[K]V seeds each map at random.
package hash

import (
	"pluto/rt"
	"unsafe"
)

// String returns a 64-bit hash of s: the hash map[string]V uses.
func String(s string, seed uint64) uint64 { return rt.StringHash(s, seed) }

// Bytes returns a 64-bit hash of b. It equals String of the same bytes.
func Bytes(b []byte, seed uint64) uint64 {
	return rt.MemHash(unsafe.Pointer(unsafe.SliceData(b)), len(b), seed)
}

// Uint64 returns a 64-bit hash of x.
func Uint64(x, seed uint64) uint64 { return rt.Uint64Hash(x, seed) }

// Float64 returns a 64-bit hash of f, giving 0 and -0 the same hash, as
// they compare equal. NaNs hash to arbitrary values (a NaN is not equal to
// itself, so a map can never find one again).
func Float64(f float64, seed uint64) uint64 {
	return rt.FloatHash(*(*uint64)(unsafe.Pointer(&f)), 8, seed)
}

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// FNV1a returns the 64-bit FNV-1a hash of b.
func FNV1a(b []byte) uint64 {
	h := uint64(fnvOffset64)
	for _, c := range b {
		h ^= uint64(c)
		h *= fnvPrime64
	}
	return h
}

// FNV1aString returns the 64-bit FNV-1a hash of s.
func FNV1aString(s string) uint64 {
	h := uint64(fnvOffset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}
