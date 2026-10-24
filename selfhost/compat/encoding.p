package compat

// The encodings of Go's standard library the compiler's packages use, which
// the Plutonium library does not have: crypto/sha256's Sum256,
// encoding/binary's varints and encoding/hex's EncodeToString
// (tools/go2pu maps the calls here).

// sha256K are SHA-256's round constants (FIPS 180-4, section 4.2.2).
var sha256K = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

func rotr32(x uint32, n uint) uint32 { return x>>n | x<<(32-n) }

// sha256Block runs the compression function over one 64-byte block.
func sha256Block(h *[8]uint32, p []byte) {
	var w [64]uint32
	for i := 0; i < 16; i++ {
		j := i * 4
		w[i] = uint32(p[j])<<24 | uint32(p[j+1])<<16 | uint32(p[j+2])<<8 | uint32(p[j+3])
	}
	for i := 16; i < 64; i++ {
		v1 := w[i-2]
		t1 := rotr32(v1, 17) ^ rotr32(v1, 19) ^ v1>>10
		v2 := w[i-15]
		t2 := rotr32(v2, 7) ^ rotr32(v2, 18) ^ v2>>3
		w[i] = t1 + w[i-7] + t2 + w[i-16]
	}
	a, b, c, d, e, f, g, hh := h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7]
	for i := 0; i < 64; i++ {
		t1 := hh + (rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25)) + (e&f ^ ^e&g) + sha256K[i] + w[i]
		t2 := (rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22)) + (a&b ^ a&c ^ b&c)
		hh = g
		g = f
		f = e
		e = d + t1
		d = c
		c = b
		b = a
		a = t1 + t2
	}
	h[0] += a
	h[1] += b
	h[2] += c
	h[3] += d
	h[4] += e
	h[5] += f
	h[6] += g
	h[7] += hh
}

// Sum256 is crypto/sha256.Sum256: the SHA-256 digest of data.
func Sum256(data []byte) [32]byte {
	h := [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}
	n := len(data)
	i := 0
	for ; i+64 <= n; i += 64 {
		sha256Block(&h, data[i:i+64])
	}
	// The last block or two: the rest, a 1 bit, zeros, the length in bits.
	var tail [128]byte
	rest := n - i
	copy(tail[:], data[i:])
	tail[rest] = 0x80
	size := 64
	if rest >= 56 {
		size = 128
	}
	bits := uint64(n) * 8
	for k := 0; k < 8; k++ {
		tail[size-1-k] = byte(bits >> (8 * uint(k)))
	}
	sha256Block(&h, tail[:64])
	if size == 128 {
		sha256Block(&h, tail[64:128])
	}
	var out [32]byte
	for k := 0; k < 8; k++ {
		out[4*k] = byte(h[k] >> 24)
		out[4*k+1] = byte(h[k] >> 16)
		out[4*k+2] = byte(h[k] >> 8)
		out[4*k+3] = byte(h[k])
	}
	return out
}

// MaxVarintLen64 is encoding/binary.MaxVarintLen64.
const MaxVarintLen64 = 10

// AppendUvarint is encoding/binary.AppendUvarint.
func AppendUvarint(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = Append(buf, byte(x)|0x80)
		x >>= 7
	}
	return Append(buf, byte(x))
}

// AppendVarint is encoding/binary.AppendVarint.
func AppendVarint(buf []byte, x int64) []byte {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return AppendUvarint(buf, ux)
}

// PutUvarint is encoding/binary.PutUvarint.
func PutUvarint(buf []byte, x uint64) int {
	i := 0
	for x >= 0x80 {
		buf[i] = byte(x) | 0x80
		x >>= 7
		i++
	}
	buf[i] = byte(x)
	return i + 1
}

// PutVarint is encoding/binary.PutVarint.
func PutVarint(buf []byte, x int64) int {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return PutUvarint(buf, ux)
}

// Uvarint is encoding/binary.Uvarint: the value and the bytes read; n is 0
// when buf is too short, and negative (minus the bytes read) on overflow.
func Uvarint(buf []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, b := range buf {
		if i == MaxVarintLen64 {
			return 0, -(i + 1) // overflow
		}
		if b < 0x80 {
			if i == MaxVarintLen64-1 && b > 1 {
				return 0, -(i + 1) // overflow
			}
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0
}

// Varint is encoding/binary.Varint.
func Varint(buf []byte) (int64, int) {
	ux, n := Uvarint(buf)
	x := int64(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x, n
}

// HexEncode is encoding/hex.EncodeToString.
func HexEncode(src []byte) string {
	const digits = "0123456789abcdef"
	out := Make[byte](2 * len(src))
	for i, b := range src {
		out[2*i] = digits[b>>4]
		out[2*i+1] = digits[b&0x0f]
	}
	return String(out)
}
