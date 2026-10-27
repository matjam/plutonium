// Translated by go2pu from internal/constant/rat.go at the go-cutover tag; edited by hand since.

package constant

import (
	"pluto/math"
	"pluto/strings"
	"compiler/compat"
)

// A rat is an exact fraction a/b in lowest terms, with b >= 1 (as a big.Rat
// in normal form).
type rat struct {
	a bigInt
	b nat
}

// makeFrac returns a/b in lowest terms; b must not be zero.
func makeFrac(a bigInt, b nat) rat {
	if len(b) == 0 {
		panic("division by zero")
	}
	if len(a.abs) == 0 {
		return rat{b: natOne}
	}
	if g := gcdNat(a.abs, b); g.cmp(natOne) != 0 {
		an, _ := divNat(a.abs, g)
		bn, _ := divNat(b, g)
		return rat{a: intOf(a.neg, an), b: bn}
	}
	return rat{a: a, b: b}
}

func ratInt(a bigInt) rat { return rat{a: a, b: natOne} }

func (x rat) isInt() bool { return x.b.cmp(natOne) == 0 }

func (x rat) sign() int { return x.a.sign() }

func (x rat) neg() rat { return rat{a: x.a.neg_(), b: x.b} }

func (x rat) add(y rat) rat {
	a := x.a.mul(bigInt{abs: y.b}).add(y.a.mul(bigInt{abs: x.b}))
	return makeFrac(a, mulNat(x.b, y.b))
}

func (x rat) sub(y rat) rat { return x.add(y.neg()) }

func (x rat) mul(y rat) rat { return makeFrac(x.a.mul(y.a), mulNat(x.b, y.b)) }

func (x rat) quo(y rat) rat {
	if len(y.a.abs) == 0 {
		panic("division by zero")
	}
	a := x.a.mul(bigInt{abs: y.b})
	b := mulNat(x.b, y.a.abs)
	return makeFrac(intOf(a.neg != y.a.neg, a.abs), b)
}

func (x rat) cmp(y rat) int {
	return x.a.mul(bigInt{abs: y.b}).cmp(y.a.mul(bigInt{abs: x.b}))
}

// String formats x as big.Rat.String does: "a/b", even for integers.
func (x rat) String() string { return compat.Concat(x.a.String(), "/", x.b.decimal()) }

// ratFloat64 returns the exact rational value of a finite float64, as
// big.Rat.SetFloat64 does.
func ratFloat64(f float64) rat {
	const expMask = 1<<11 - 1
	b := math.Float64bits(f)
	mantissa := b & (1<<52 - 1)
	exp := int((b >> 52) & expMask)
	switch exp {
	case expMask:
		panic("constant: ratFloat64 of a non-finite number")
	case 0: // denormal
		exp -= 1022
	default: // normal
		mantissa |= 1 << 52
		exp -= 1023
	}
	shift := 52 - exp
	for mantissa&1 == 0 && shift > 0 {
		mantissa >>= 1
		shift--
	}
	a := intOf(f < 0, natUint64(mantissa))
	if shift > 0 {
		return makeFrac(a, shlNat(natOne, uint(shift)))
	}
	return makeFrac(a.lsh(uint(-shift)), natOne)
}

// quotToFloat returns the float nearest a/b (a, b > 0) with a mantissa of
// msize bits plus the implicit one and an exponent range given by ebias,
// rounding half to even, and whether it is exact. It is big's quotToFloat64
// and quotToFloat32, merged; f64 selects the float64 result.
func quotToFloat(a, b nat, f64 bool) (float64, bool) {
	msize, ebias := 23, 127
	if f64 {
		msize, ebias = 52, 1023
	}
	msize1 := msize + 1
	msize2 := msize1 + 1
	emin := 1 - ebias

	alen := a.bitLen()
	if alen == 0 {
		return 0, true
	}
	blen := b.bitLen()
	if blen == 0 {
		panic("division by zero")
	}
	// Shift a or b so that a/b is in [1<<msize1, 1<<(msize2+1)).
	exp := alen - blen
	a2, b2 := a, b
	if shift := msize2 - exp; shift > 0 {
		a2 = shlNat(a2, uint(shift))
	} else if shift < 0 {
		b2 = shlNat(b2, uint(-shift))
	}
	q, r := divNat(a2, b2)
	mantissa := q.low64()
	haveRem := len(r) > 0
	if mantissa>>uint(msize2) == 1 {
		if mantissa&1 == 1 {
			haveRem = true
		}
		mantissa >>= 1
		exp++
	}
	if mantissa>>uint(msize1) != 1 {
		panic("constant: quotToFloat: wrong mantissa size")
	}
	// Denormals lose shift bits of precision.
	if emin-msize <= exp && exp <= emin {
		shift := uint(emin - (exp - 1))
		lostbits := mantissa & (1<<shift - 1)
		haveRem = haveRem || lostbits != 0
		mantissa >>= shift
		exp = 2 - ebias
	}
	exact := !haveRem
	if mantissa&1 != 0 {
		exact = false
		if haveRem || mantissa&2 != 0 {
			if mantissa++; mantissa >= 1<<uint(msize2) {
				mantissa >>= 1
				exp++
			}
		}
	}
	mantissa >>= 1 // drop the rounding bit
	if f64 {
		f := math.Ldexp(float64(mantissa), exp-msize1)
		if math.IsInf(f, 0) {
			exact = false
		}
		return f, exact
	}
	f := float32(math.Ldexp(float64(mantissa), exp-msize1))
	if math.IsInf(float64(f), 0) {
		exact = false
	}
	return float64(f), exact
}

// float64 returns the float64 nearest x and whether it is exact.
func (x rat) float64() (float64, bool) {
	f, exact := quotToFloat(x.a.abs, x.b, true)
	if x.a.neg {
		f = -f
	}
	return f, exact
}

// float32 returns the float32 nearest x and whether it is exact.
func (x rat) float32() (float32, bool) {
	f, exact := quotToFloat(x.a.abs, x.b, false)
	if x.a.neg {
		f = -f
	}
	return float32(f), exact
}

// ratFromString parses s as big.Rat.SetString does for a floating-point
// number: a radix point, a decimal "e" or binary "p" exponent, and base
// prefixes. (big.Rat also accepts fractions "a/b"; literals never are.)
func ratFromString(s string) (rat, bool) {
	neg, s, ok := scanSign(s)
	if !ok {
		return rat{}, false
	}
	mant, base, fcount, rest, ok := natScan(s, true)
	if !ok {
		return rat{}, false
	}
	exp, ebase, rest, ok := scanExponent(rest, true)
	if !ok || rest != "" {
		return rat{}, false
	}
	if len(mant) == 0 {
		return rat{b: natOne}, true
	}
	exp2, exp5 := fracExponents(base, fcount)
	switch ebase {
	case 10:
		exp5 += exp
		exp2 += exp
	case 2:
		exp2 += exp
	}
	a, b := mant, natOne
	if exp5 != 0 {
		n := exp5
		if n < 0 {
			n = -n
			if n < 0 {
				return rat{}, false
			}
		}
		if n > 1e6 {
			return rat{}, false
		}
		p := powNat(nat{5}, uint64(n))
		if exp5 > 0 {
			a = mulNat(a, p)
		} else {
			b = p
		}
	}
	if exp2 < -1e7 || exp2 > 1e7 {
		return rat{}, false
	}
	if exp2 > 0 {
		a = shlNat(a, uint(exp2))
	} else if exp2 < 0 {
		b = shlNat(b, uint(-exp2))
	}
	return makeFrac(intOf(neg, a), b), true
}

// fracExponents returns the binary and decimal exponents a radix point
// contributes: fcount <= 0 digits after the point in the given base.
func fracExponents(base, fcount int) (exp2, exp5 int64) {
	if fcount >= 0 {
		return 0, 0
	}
	d := int64(fcount)
	switch base {
	case 10:
		return d, d
	case 2:
		return d, 0
	case 8:
		return d * 3, 0
	case 16:
		return d * 4, 0
	}
	panic("constant: unexpected mantissa base")
}

// scanExponent scans an optional exponent at the start of s, as big's
// scanExponent does with underscores allowed: "e" or "E" for a decimal
// exponent, "p" or "P" (if base2ok) for a binary one.
func scanExponent(s string, base2ok bool) (exp int64, base int, rest string, ok bool) {
	if s == "" {
		return 0, 10, s, true
	}
	switch s[0] {
	case 'e', 'E':
		base = 10
	case 'p', 'P':
		if !base2ok {
			return 0, 10, s, true
		}
		base = 2
	default:
		return 0, 10, s, true
	}
	i := 1
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	prev := byte('.')
	invalSep := false
	hasDigits := false
	overflow := false
	var v uint64
	for ; i < len(s); i++ {
		ch := s[i]
		if '0' <= ch && ch <= '9' {
			if v > (1<<63)/10 {
				overflow = true
			}
			v = v*10 + uint64(ch-'0')
			if v > 1<<63 {
				overflow = true
			}
			prev = '0'
			hasDigits = true
		} else if ch == '_' {
			if prev != '0' {
				invalSep = true
			}
			prev = '_'
		} else {
			break
		}
	}
	if !hasDigits || overflow || invalSep || prev == '_' {
		return 0, base, strings.SliceFrom(s, i), false
	}
	if v == 1<<63 && !neg {
		return 0, base, strings.SliceFrom(s, i), false
	}
	exp = int64(v)
	if neg {
		exp = -exp
	}
	return exp, base, strings.SliceFrom(s, i), true
}
