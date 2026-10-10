package opt

import (
	"math/big"
	"math/bits"
)

// Division by constants as multiplication by a "magic" reciprocal, after
// Granlund and Montgomery, "Division by Invariant Integers using
// Multiplication", in the formulation of Go's compiler (magic.go). All
// arithmetic is on 64-bit operands; narrower divisions are widened first.

// umagic returns s and m such that for every 64-bit unsigned x,
//
//	x / c = (x * (2^64 + m)) >> (64 + s)
//
// for c > 1 not a power of two.
func umagic(c uint64) (s uint, m uint64) {
	C := new(big.Int).SetUint64(c)
	s = uint(C.BitLen())
	M := big.NewInt(1)
	M.Lsh(M, 64+s)
	M.Add(M, C)
	M.Sub(M, big.NewInt(1))
	M.Div(M, C) // ceil(2^(64+s) / c)
	M.SetBit(M, 64, 0)
	return s, M.Uint64()
}

// smagic returns s and m such that for every 64-bit signed x and c > 1 not
// a power of two,
//
//	x / c = (x * m) >> (64 + s)  (rounded toward zero by adding the sign)
//
// where m < 2^64 is read as unsigned.
func smagic(c int64) (s uint, m uint64) {
	C := big.NewInt(c)
	s = uint(C.BitLen() - 1)
	M := big.NewInt(1)
	M.Lsh(M, 64+s)
	M.Add(M, C)
	M.Sub(M, big.NewInt(1))
	M.Div(M, C) // ceil(2^(64+s) / c)
	return s, M.Uint64()
}

// isPowerOfTwo reports whether c is a positive power of two and returns its
// exponent.
func isPowerOfTwo(c uint64) (uint, bool) {
	if c == 0 || c&(c-1) != 0 {
		return 0, false
	}
	return uint(bits.TrailingZeros64(c)), true
}
