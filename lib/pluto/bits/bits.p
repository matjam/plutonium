// Package bits provides bit counting and manipulation, following Go's
// math/bits, plus operations that change bits of a variable in place.
package bits

// Unsigned is any unsigned integer type.
type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

const deBruijn64 = 0x03f79d71b4ca8b09

var deBruijnIdx64 = [64]byte{
	0, 1, 56, 2, 57, 49, 28, 3, 61, 58, 42, 50, 38, 29, 17, 4,
	62, 47, 59, 36, 45, 43, 51, 22, 53, 39, 33, 30, 24, 18, 12, 5,
	63, 55, 48, 27, 60, 41, 37, 16, 46, 35, 44, 21, 52, 32, 23, 11,
	54, 26, 40, 15, 34, 20, 31, 10, 25, 14, 19, 9, 13, 8, 7, 6,
}

// OnesCount64 returns the number of one bits in x.
func OnesCount64(x uint64) int {
	const m0 = 0x5555555555555555
	const m1 = 0x3333333333333333
	const m2 = 0x0f0f0f0f0f0f0f0f
	x = x>>1&m0 + x&m0
	x = x>>2&m1 + x&m1
	x = (x>>4 + x) & m2
	return int((x * 0x0101010101010101) >> 56)
}

// OnesCount32 returns the number of one bits in x.
func OnesCount32(x uint32) int { return OnesCount64(uint64(x)) }

// OnesCount16 returns the number of one bits in x.
func OnesCount16(x uint16) int { return OnesCount64(uint64(x)) }

// OnesCount8 returns the number of one bits in x.
func OnesCount8(x uint8) int { return OnesCount64(uint64(x)) }

// OnesCount returns the number of one bits in x.
func OnesCount(x uint) int { return OnesCount64(uint64(x)) }

// TrailingZeros64 returns the number of trailing zero bits in x; 64 for 0.
func TrailingZeros64(x uint64) int {
	if x == 0 {
		return 64
	}
	return int(deBruijnIdx64[(x&-x)*deBruijn64>>(64-6)])
}

// TrailingZeros32 returns the number of trailing zero bits in x; 32 for 0.
func TrailingZeros32(x uint32) int {
	if x == 0 {
		return 32
	}
	return TrailingZeros64(uint64(x))
}

// TrailingZeros returns the number of trailing zero bits in x.
func TrailingZeros(x uint) int { return TrailingZeros64(uint64(x)) }

// Len64 returns the minimum number of bits to represent x; 0 for 0.
func Len64(x uint64) int {
	n := 0
	if x >= 1<<32 {
		x >>= 32
		n = 32
	}
	if x >= 1<<16 {
		x >>= 16
		n += 16
	}
	if x >= 1<<8 {
		x >>= 8
		n += 8
	}
	for x != 0 {
		x >>= 1
		n++
	}
	return n
}

// Len32 returns the minimum number of bits to represent x.
func Len32(x uint32) int { return Len64(uint64(x)) }

// Len returns the minimum number of bits to represent x.
func Len(x uint) int { return Len64(uint64(x)) }

// LeadingZeros64 returns the number of leading zero bits in x; 64 for 0.
func LeadingZeros64(x uint64) int { return 64 - Len64(x) }

// LeadingZeros32 returns the number of leading zero bits in x; 32 for 0.
func LeadingZeros32(x uint32) int { return 32 - Len32(x) }

// LeadingZeros returns the number of leading zero bits in x.
func LeadingZeros(x uint) int { return 64 - Len(x) }

// RotateLeft64 rotates x left by k bits (right for negative k).
func RotateLeft64(x uint64, k int) uint64 {
	s := uint(k) & 63
	return x<<s | x>>(64-s)
}

// RotateLeft32 rotates x left by k bits (right for negative k).
func RotateLeft32(x uint32, k int) uint32 {
	s := uint(k) & 31
	return x<<s | x>>(32-s)
}

// RotateLeft rotates x left by k bits.
func RotateLeft(x uint, k int) uint { return uint(RotateLeft64(uint64(x), k)) }

// ReverseBytes64 reverses the byte order of x.
func ReverseBytes64(x uint64) uint64 {
	const m3 = 0x00ff00ff00ff00ff
	const m4 = 0x0000ffff0000ffff
	x = x>>8&m3 | x&m3<<8
	x = x>>16&m4 | x&m4<<16
	return x>>32 | x<<32
}

// ReverseBytes32 reverses the byte order of x.
func ReverseBytes32(x uint32) uint32 { return uint32(ReverseBytes64(uint64(x)) >> 32) }

// ReverseBytes16 reverses the byte order of x.
func ReverseBytes16(x uint16) uint16 { return x>>8 | x<<8 }

// Add64 returns x + y + carry and the carry out (carry must be 0 or 1).
func Add64(x, y, carry uint64) (sum, carryOut uint64) {
	sum = x + y + carry
	carryOut = ((x & y) | ((x | y) &^ sum)) >> 63
	return
}

// Sub64 returns x - y - borrow and the borrow out.
func Sub64(x, y, borrow uint64) (diff, borrowOut uint64) {
	diff = x - y - borrow
	borrowOut = ((^x & y) | (^(x ^ y) & diff)) >> 63
	return
}

// Mul64 returns the 128-bit product of x and y as (hi, lo).
func Mul64(x, y uint64) (hi, lo uint64) {
	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + w0>>32
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	hi = x1*y1 + w2 + w1>>32
	lo = x * y
	return
}

// ----------------------------------------------------------------------------
// In-place bit manipulation

// Set sets bit i of *x.
func Set[T Unsigned](x *T, i uint) { *x |= T(1) << i }

// Clear clears bit i of *x.
func Clear[T Unsigned](x *T, i uint) { *x &^= T(1) << i }

// Toggle flips bit i of *x.
func Toggle[T Unsigned](x *T, i uint) { *x ^= T(1) << i }

// Test reports whether bit i of x is set.
func Test[T Unsigned](x T, i uint) bool { return x>>i&1 != 0 }

// Extract returns the width bits of x starting at bit lo.
func Extract[T Unsigned](x T, lo, width uint) T {
	return x >> lo & (T(1)<<width - 1)
}

// Insert replaces the width bits of *x starting at bit lo with the low bits
// of v.
func Insert[T Unsigned](x *T, lo, width uint, v T) {
	mask := (T(1)<<width - 1) << lo
	*x = *x&^mask | v<<lo&mask
}
