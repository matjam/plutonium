// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Floating point decimal to binary conversion by fast unrounded scaling,
// from Go 1.27's internal/strconv/uscale.go. See "Floating-Point Printing
// and Parsing Can Be Simple And Fast", https://research.swtch.com/fp
//
// Formatting (ftoa.p) uses multiprecision decimals, which give the same
// digits; the powers of ten for parsing are the table in package rt, which
// prints floats with the same algorithm.

package strconv

import "pluto/bits"

// bool2 converts b to an integer: 1 for true, 0 for false.
func bool2[T ~int | ~uint32 | ~uint64](b bool) T {
	if b {
		return 1
	}
	return 0
}

// pack64 takes m, e and returns f = m * 2**e.
// It assumes the caller has provided a 53-bit mantissa m
// and an exponent that is in range for the mantissa.
func pack64(m uint64, e int) (float64, error) {
	if m&(1<<52) == 0 {
		return float64frombits(m), nil
	}
	if e >= 0x7FF-1075 {
		return float64frombits(m&(1<<63) | 0x7ff<<52), ErrRange
	}
	return float64frombits(m&^(1<<52) | uint64(1075+e)<<52), nil
}

// pack32 takes m, e and returns f = m * 2**e.
// It assumes the caller has provided a 24-bit mantissa m
// and an exponent that is in range for the mantissa.
func pack32(m uint32, e int) (float32, error) {
	if m&(1<<23) == 0 {
		return float32frombits(m), nil
	}
	if e >= 0xFF-150 {
		return float32frombits(m&(1<<31) | 0xff<<23), ErrRange
	}
	return float32frombits(m&^(1<<23) | uint32(150+e)<<23), nil
}

// An unrounded represents an unrounded value: the integer part in the
// high 62 bits, then a half bit, then a sticky bit set when anything
// non-zero lies below it.
type unrounded uint64

func (u unrounded) round() uint64 { return uint64((u + 1 + (u>>2)&1) >> 2) }

func (u unrounded) rsh(s int) unrounded {
	return u>>s | u&1 | bool2[unrounded](u&((1<<s)-1) != 0)
}

// log2Pow10(x) returns ⌊log₂ 10**x⌋ = ⌊x * log₂ 10⌋.
func log2Pow10(x int) int {
	// log₂ 10 ≈ 3.32192809489 ≈ 108853 / 2^15
	return (x * 108853) >> 15
}

// parseFloat64 rounds d * 10**p to the nearest float64 f.
// d can have at most 19 digits.
// It returns ErrRange if the result rounds to infinity.
func parseFloat64(d uint64, p int, sign uint64) (float64, error) {
	b := bits.Len64(d)
	lp := log2Pow10(p)
	e := min(1074, 53-b-lp)
	var pre scaler
	prescale(&pre, e-(64-b), p, lp)
	if pre.s >= 64 {
		return float64frombits(sign | 0), nil
	}
	u := uscale(d<<(64-b), &pre)

	// This block is branch-free code for:
	//	if u.round() >= 1<<53 {
	//		u = u.rsh(1)
	//		e = e - 1
	//	}
	s := bool2[int](u >= unmin(1<<53))
	u = u>>s | u&1
	e = e - s

	return pack64(sign|u.round(), -e)
}

// parseFloat32 rounds d * 10**p to the nearest float32 f.
// d can have at most 19 digits.
// It returns ErrRange if the result rounds to infinity.
func parseFloat32(d uint64, p int, sign uint32) (float32, error) {
	b := bits.Len64(d)
	lp := log2Pow10(p)
	e := min(149, 24-b-lp)
	var pre scaler
	prescale(&pre, e-(64-b), p, lp)
	if pre.s >= 64 {
		return float32frombits(sign | 0), nil
	}
	u := uscale(d<<(64-b), &pre)

	// This block is branch-free code for:
	//	if u.round() >= 1<<24 {
	//		u = u.rsh(1)
	//		e = e - 1
	//	}
	s := bool2[int](u >= unmin(1<<24))
	u = u>>s | u&1
	e = e - s

	return pack32(sign|uint32(u.round()), -e)
}

// unmin returns the minimum unrounded that rounds to x.
func unmin(x uint64) unrounded {
	return unrounded(x<<2 - 2)
}

// A scaler holds derived scaling constants for a given e, p pair.
type scaler struct {
	pmHi uint64
	pmLo uint64
	s    int
}

// prescale returns the scaling constants for e, p.
// lp must be log2Pow10(p).
// The caller is responsible for either avoiding e, p pairs
// that cause pre.s < 0 or pre.s >= 64, or else handling
// those cases before passing the result to uscale.
// In practice, pre.s < 0 would indicate a buggy caller
// and pre.s >= 64 can only happen for parsing and is
// picked off at those call sites.
func prescale(pre *scaler, e, p, lp int) {
	pre.pmHi, pre.pmLo = pow10(p)
	pre.s = -(e + lp + 3)
}

// uscale returns unround(x * 2**e * 10**p).
// The caller should pass &pre for prescale(&pre, e, p, log2Pow10(p))
// and should have left-justified x so its high bit is set.
// The caller is also responsible for checking that c.s < 64.
// For parsing, the caller needs to pick it off early and return a signed 0.
func uscale(x uint64, c *scaler) unrounded {
	hi, mid := bits.Mul64(x, c.pmHi)
	s := c.s & 63 // make shifts cheaper
	if hi>>s<<s != hi {
		return unrounded(hi>>s | 1)
	}
	mid2, _ := bits.Mul64(x, c.pmLo)
	hi -= bool2[uint64](mid < mid2)
	return unrounded(hi>>s | bool2[uint64](mid-mid2 > 1))
}
