// Package hash provides hash functions and a hash map and set that take an
// explicit allocator.
//
// The hash functions are not cryptographic. String, Bytes and Uint64 are
// fast mixing functions in the style of wyhash; FNV1a and FNV1aString are
// the classic 64-bit FNV-1a, for formats that specify it.
//
// Map and Set are open-addressing tables with linear probing. They take a
// seed but, with no source of randomness in the library, use a fixed one by
// default: do not use them to index attacker-controlled keys without a
// secret seed (InitSeed) or a keyed hash function.
package hash

import (
	"pluto/bits"
	"unsafe"
)

// Mixing constants from wyhash (public domain).
const (
	wyp0 = 0xa0761d6478bd642f
	wyp1 = 0xe7037ed1a0b428db
	wyp2 = 0x8ebc6af09c88c6e3
	wyp3 = 0x589965cc75374cc3
)

// mix multiplies a and b to 128 bits and folds the halves together.
func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return hi ^ lo
}

// load64 reads 8 bytes at p+i, little-endian, without alignment.
func load64(p unsafe.Pointer, i int) uint64 {
	b := (*[8]byte)(unsafe.Add(p, i))
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// load32 reads 4 bytes at p+i, little-endian, without alignment.
func load32(p unsafe.Pointer, i int) uint64 {
	b := (*[4]byte)(unsafe.Add(p, i))
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24
}

// memhash hashes n bytes at p.
func memhash(p unsafe.Pointer, n int, seed uint64) uint64 {
	seed ^= wyp0
	var a, b uint64
	switch {
	case n == 0:
		return mix(seed^wyp1, wyp1)
	case n < 4:
		q := (*[3]byte)(p)
		a = uint64(q[0])<<16 | uint64(q[n>>1])<<8 | uint64(q[n-1])
	case n <= 8:
		a = load32(p, 0)
		b = load32(p, n-4)
	case n <= 16:
		a = load64(p, 0)
		b = load64(p, n-8)
	default:
		i := 0
		for ; n-i > 16; i += 16 {
			seed = mix(load64(p, i)^wyp1, load64(p, i+8)^seed)
		}
		a = load64(p, n-16)
		b = load64(p, n-8)
	}
	return mix(wyp1^uint64(n), mix(a^wyp1, b^seed))
}

// String returns a 64-bit hash of s.
func String(s string, seed uint64) uint64 {
	if len(s) == 0 {
		return memhash(nil, 0, seed)
	}
	return memhash(unsafe.Pointer(unsafe.StringData(s)), len(s), seed)
}

// Bytes returns a 64-bit hash of b. It equals String of the same bytes.
func Bytes(b []byte, seed uint64) uint64 {
	if len(b) == 0 {
		return memhash(nil, 0, seed)
	}
	return memhash(unsafe.Pointer(unsafe.SliceData(b)), len(b), seed)
}

// Uint64 returns a 64-bit hash of x.
func Uint64(x, seed uint64) uint64 {
	return mix(mix(x^wyp0, seed^wyp1), wyp2^8)
}

// Float64 returns a 64-bit hash of f, giving 0 and -0 the same hash, as
// they compare equal. NaNs hash to arbitrary values (a NaN is not equal to
// itself, so a map can never find one again).
func Float64(f float64, seed uint64) uint64 {
	if f == 0 {
		f = 0 // -0 becomes +0
	}
	return Uint64(*(*uint64)(unsafe.Pointer(&f)), seed)
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
