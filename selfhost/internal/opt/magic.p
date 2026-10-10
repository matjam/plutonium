// Hand-written port of internal/opt/magic.go, which go2pu does not
// translate because it uses math/big. The numbers involved are below 2^129,
// so a fixed three-word integer and restoring long division replace big.Int
// (see "math/big" in docs/plans/2026-10-09-self-hosting-plan.md).

package opt

import "pluto/bits"

// Division by constants as multiplication by a "magic" reciprocal, after
// Granlund and Montgomery, "Division by Invariant Integers using
// Multiplication", in the formulation of Go's compiler (magic.go). All
// arithmetic is on 64-bit operands; narrower divisions are widened first.

// u192 is a 192-bit unsigned integer, least significant word first.
type u192 [3]uint64

func (a u192) shl1() u192 { return u192{a[0] << 1, a[1]<<1 | a[0]>>63, a[2]<<1 | a[1]>>63} }

func (a u192) geq(b u192) bool {
	for i := 2; i >= 0; i-- {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

func (a u192) sub(b u192) u192 {
	r0, c := bits.Sub64(a[0], b[0], 0)
	r1, c := bits.Sub64(a[1], b[1], c)
	r2, _ := bits.Sub64(a[2], b[2], c)
	return u192{r0, r1, r2}
}

func (a u192) add(b u192) u192 {
	r0, c := bits.Add64(a[0], b[0], 0)
	r1, c := bits.Add64(a[1], b[1], c)
	r2, _ := bits.Add64(a[2], b[2], c)
	return u192{r0, r1, r2}
}

// div returns a / b for b != 0.
func (a u192) div(b u192) u192 {
	var q, r u192
	for i := 191; i >= 0; i-- {
		r = r.shl1()
		r[0] |= a[i/64] >> (i % 64) & 1
		if r.geq(b) {
			r = r.sub(b)
			q[i/64] |= 1 << (i % 64)
		}
	}
	return q
}

// ceilPow returns ceil(2^(64+s) / c) for 0 < c and s <= 64.
func ceilPow(s uint, c uint64) u192 {
	var num u192
	k := 64 + s
	num[k/64] = 1 << (k % 64)
	num = num.add(u192{c - 1, 0, 0})
	return num.div(u192{c, 0, 0})
}

// umagic returns s and m such that for every 64-bit unsigned x,
//
//	x / c = (x * (2^64 + m)) >> (64 + s)
//
// for c > 1 not a power of two.
func umagic(c uint64) (s uint, m uint64) {
	s = uint(bits.Len64(c))
	return s, ceilPow(s, c)[0]
}

// smagic returns s and m such that for every 64-bit signed x and c > 1 not
// a power of two,
//
//	x / c = (x * m) >> (64 + s)  (rounded toward zero by adding the sign)
//
// where m < 2^64 is read as unsigned.
func smagic(c int64) (s uint, m uint64) {
	s = uint(bits.Len64(uint64(c)) - 1)
	return s, ceilPow(s, uint64(c))[0]
}

// isPowerOfTwo reports whether c is a positive power of two and returns its
// exponent.
func isPowerOfTwo(c uint64) (uint, bool) {
	if c == 0 || c&(c-1) != 0 {
		return 0, false
	}
	return uint(bits.TrailingZeros64(c)), true
}
