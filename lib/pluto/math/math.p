// Package math provides floating-point constants and functions.
//
// Sqrt, Abs and the bit conversions compile to single instructions.
// Parts are adapted from Go's math package.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package math

import "unsafe"

// Mathematical constants.
const (
	E   = 2.71828182845904523536028747135266249775724709369995957496696763
	Pi  = 3.14159265358979323846264338327950288419716939937510582097494459
	Phi = 1.61803398874989484820458683436563811772030917980576286213544862

	Sqrt2  = 1.41421356237309504880168872420969807856967187537694807317667974
	SqrtE  = 1.64872127070012814684865078831848487198362813359216106766024418
	SqrtPi = 1.77245385090551602729816748334114518279754945612238712821380779
	Ln2    = 0.693147180559945309417232121458176568075500134360255254120680009
	Log2E  = 1 / Ln2
	Ln10   = 2.30258509299404568401799145468436420760110148862877297603332790
	Log10E = 1 / Ln10
)

// Limits.
const (
	MaxFloat64             = 0x1p1023 * (1 + (1 - 0x1p-52))
	SmallestNonzeroFloat64 = 0x1p-1022 * 0x1p-52
	MaxFloat32             = 0x1p127 * (1 + (1 - 0x1p-23))
	SmallestNonzeroFloat32 = 0x1p-126 * 0x1p-23

	MaxInt    = 1<<63 - 1
	MinInt    = -1 << 63
	MaxInt8   = 1<<7 - 1
	MinInt8   = -1 << 7
	MaxInt16  = 1<<15 - 1
	MinInt16  = -1 << 15
	MaxInt32  = 1<<31 - 1
	MinInt32  = -1 << 31
	MaxInt64  = 1<<63 - 1
	MinInt64  = -1 << 63
	MaxUint   = 1<<64 - 1
	MaxUint8  = 1<<8 - 1
	MaxUint16 = 1<<16 - 1
	MaxUint32 = 1<<32 - 1
	MaxUint64 = 1<<64 - 1
)

// Float64bits returns the IEEE 754 bits of f.
func Float64bits(f float64) uint64 { return *(*uint64)(unsafe.Pointer(&f)) }

// Float64frombits returns the float64 with bits b.
func Float64frombits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }

// Float32bits returns the IEEE 754 bits of f.
func Float32bits(f float32) uint32 { return *(*uint32)(unsafe.Pointer(&f)) }

// Float32frombits returns the float32 with bits b.
func Float32frombits(b uint32) float32 { return *(*float32)(unsafe.Pointer(&b)) }

// Sqrt returns the square root of x (a single instruction).
func Sqrt(x float64) float64 {
	// Newton's method fallback; the compiler replaces calls with sqrtsd.
	if x <= 0 || IsInf(x, 1) || x != x {
		if x == 0 {
			return x
		}
		return NaN()
	}
	z := x
	for i := 0; i < 60; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// Abs returns |x|.
func Abs(x float64) float64 { return Float64frombits(Float64bits(x) &^ (1 << 63)) }

// Copysign returns a value with the magnitude of f and the sign of sign.
func Copysign(f, sign float64) float64 {
	const signBit = 1 << 63
	return Float64frombits(Float64bits(f)&^signBit | Float64bits(sign)&signBit)
}

// Signbit reports whether x is negative or negative zero.
func Signbit(x float64) bool { return Float64bits(x)&(1<<63) != 0 }

// Inf returns positive infinity if sign >= 0, negative infinity otherwise.
func Inf(sign int) float64 {
	if sign >= 0 {
		return Float64frombits(0x7FF0000000000000)
	}
	return Float64frombits(0xFFF0000000000000)
}

// NaN returns a quiet NaN.
func NaN() float64 { return Float64frombits(0x7FF8000000000001) }

// IsNaN reports whether f is a NaN.
func IsNaN(f float64) bool { return f != f }

// IsInf reports whether f is an infinity of the given sign (any if 0).
func IsInf(f float64, sign int) bool {
	return sign >= 0 && f > MaxFloat64 || sign <= 0 && f < -MaxFloat64
}

const (
	mask  = 0x7FF
	shift = 64 - 11 - 1
	bias  = 1023
)

// Trunc returns the integer part of x.
func Trunc(x float64) float64 {
	if x == 0 || IsNaN(x) || IsInf(x, 0) {
		return x
	}
	d, _ := Modf(x)
	return d
}

// Floor returns the greatest integer value <= x.
func Floor(x float64) float64 {
	if x == 0 || IsNaN(x) || IsInf(x, 0) {
		return x
	}
	if x < 0 {
		d, fract := Modf(-x)
		if fract != 0 {
			d = d + 1
		}
		return -d
	}
	d, _ := Modf(x)
	return d
}

// Ceil returns the least integer value >= x.
func Ceil(x float64) float64 { return -Floor(-x) }

// Round returns the nearest integer, rounding half away from zero.
func Round(x float64) float64 {
	bits := Float64bits(x)
	e := uint(bits>>shift) & mask
	if e < bias {
		bits &= 1 << 63
		if e == bias-1 {
			bits |= 0x3FF0000000000000
		}
	} else if e < bias+shift {
		const half = 1 << (shift - 1)
		e -= bias
		bits += half >> e
		bits &^= (1<<shift - 1) >> e
	}
	return Float64frombits(bits)
}

// Modf returns the integer and fractional parts of f, both with f's sign.
func Modf(f float64) (int float64, frac float64) {
	if f < 1 {
		switch {
		case f < 0:
			int, frac = Modf(-f)
			return -int, -frac
		case f == 0:
			return f, f
		}
		return 0, f
	}
	x := Float64bits(f)
	e := uint(x>>shift)&mask - bias
	if e < 64-12 {
		x &^= 1<<(64-12-e) - 1
	}
	int = Float64frombits(x)
	frac = f - int
	return
}

// Mod returns the floating-point remainder of x/y, with x's sign.
func Mod(x, y float64) float64 {
	if y == 0 || IsInf(x, 0) || IsNaN(x) || IsNaN(y) {
		return NaN()
	}
	y = Abs(y)
	yfr, yexp := Frexp(y)
	r := x
	if x < 0 {
		r = -x
	}
	for r >= y {
		rfr, rexp := Frexp(r)
		if rfr < yfr {
			rexp = rexp - 1
		}
		r = r - Ldexp(y, rexp-yexp)
	}
	if x < 0 {
		r = -r
	}
	return r
}

// Frexp breaks f into a fraction in [0.5, 1) and a power of two.
func Frexp(f float64) (frac float64, exp int) {
	switch {
	case f == 0:
		return f, 0
	case IsInf(f, 0) || IsNaN(f):
		return f, 0
	}
	if Abs(f) < 0x1p-1022 { // subnormal
		f *= 1 << 52
		exp = -52
	}
	x := Float64bits(f)
	exp += int((x>>shift)&mask) - bias + 1
	x &^= mask << shift
	x |= (-1 + bias) << shift
	frac = Float64frombits(x)
	return
}

// Ldexp returns frac * 2**exp.
func Ldexp(frac float64, exp int) float64 {
	switch {
	case frac == 0:
		return frac
	case IsInf(frac, 0) || IsNaN(frac):
		return frac
	}
	frac, e := Frexp(frac)
	exp += e
	x := Float64bits(frac)
	exp += int(x>>shift)&mask - bias
	if exp < -1075 {
		return Copysign(0, frac)
	}
	if exp > 1023 {
		if frac < 0 {
			return Inf(-1)
		}
		return Inf(1)
	}
	var m float64 = 1
	if exp < -1022 {
		exp += 53
		m = 1.0 / (1 << 53)
	}
	x &^= mask << shift
	x |= uint64(exp+bias) << shift
	return m * Float64frombits(x)
}
