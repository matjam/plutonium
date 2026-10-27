// Translated by go2pu from internal/constant/int.go at the go-cutover tag; edited by hand since.

package constant

import (
	"compiler/compat"
	"pluto/strings"
)

// A bigInt is a signed integer of any size: a sign and a magnitude. Zero is
// never negative. Like nat, operations return new values.
type bigInt struct {
	neg bool
	abs nat
}

func intOf(neg bool, abs nat) bigInt {
	abs = abs.norm()
	return bigInt{neg: neg && len(abs) > 0, abs: abs}
}

func int64ToInt(x int64) bigInt {
	if x < 0 {
		return bigInt{neg: true, abs: natUint64(uint64(-x))}
	}
	return bigInt{abs: natUint64(uint64(x))}
}

func (x bigInt) sign() int {
	switch {
	case len(x.abs) == 0:
		return 0
	case x.neg:
		return -1
	}
	return 1
}

func (x bigInt) cmp(y bigInt) int {
	switch {
	case x.neg && !y.neg:
		return -1
	case !x.neg && y.neg:
		return 1
	}
	c := x.abs.cmp(y.abs)
	if x.neg {
		return -c
	}
	return c
}

func (x bigInt) neg_() bigInt { return intOf(!x.neg, x.abs) }

func (x bigInt) add(y bigInt) bigInt {
	if x.neg == y.neg {
		return intOf(x.neg, addNat(x.abs, y.abs))
	}
	if x.abs.cmp(y.abs) >= 0 {
		return intOf(x.neg, subNat(x.abs, y.abs))
	}
	return intOf(y.neg, subNat(y.abs, x.abs))
}

func (x bigInt) sub(y bigInt) bigInt { return x.add(y.neg_()) }

func (x bigInt) mul(y bigInt) bigInt { return intOf(x.neg != y.neg, mulNat(x.abs, y.abs)) }

// quoRem returns the quotient truncated toward zero and the remainder with
// the sign of x, as Go's / and % do.
func (x bigInt) quoRem(y bigInt) (bigInt, bigInt) {
	q, r := divNat(x.abs, y.abs)
	return intOf(x.neg != y.neg, q), intOf(x.neg, r)
}

func (x bigInt) isInt64() bool {
	if len(x.abs) > 1 {
		return false
	}
	w := x.abs.low64()
	return int64(w) >= 0 || x.neg && w == 1<<63
}

// int64 returns the low 64 bits of x as an int64 (in two's complement).
func (x bigInt) int64() int64 {
	v := int64(x.abs.low64())
	if x.neg {
		v = -v
	}
	return v
}

func (x bigInt) isUint64() bool { return !x.neg && len(x.abs) <= 1 }

// uint64 returns the low 64 bits of |x|, as big.Int.Uint64 does.
func (x bigInt) uint64() uint64 { return x.abs.low64() }

// The bitwise operations treat x and y as infinite two's complement
// numbers, as big.Int does: for x < 0, ^x == -x-1 == |x|-1.

func (x bigInt) not() bigInt {
	if x.neg {
		return intOf(false, subNat(x.abs, natOne))
	}
	return intOf(true, addNat(x.abs, natOne))
}

func (x bigInt) and(y bigInt) bigInt {
	switch {
	case !x.neg && !y.neg:
		return intOf(false, andNat(x.abs, y.abs))
	case x.neg && y.neg:
		// (-x) & (-y) == ^(x-1) & ^(y-1) == ^((x-1) | (y-1)) == -(((x-1) | (y-1)) + 1)
		x1, y1 := subNat(x.abs, natOne), subNat(y.abs, natOne)
		return intOf(true, addNat(orNat(x1, y1), natOne))
	}
	if x.neg {
		x, y = y, x
	}
	// x & (-y) == x & ^(y-1) == x &^ (y-1)
	return intOf(false, andNotNat(x.abs, subNat(y.abs, natOne)))
}

func (x bigInt) andNot(y bigInt) bigInt {
	switch {
	case !x.neg && !y.neg:
		return intOf(false, andNotNat(x.abs, y.abs))
	case x.neg && y.neg:
		// (-x) &^ (-y) == ^(x-1) &^ ^(y-1) == ^(x-1) & (y-1) == (y-1) &^ (x-1)
		x1, y1 := subNat(x.abs, natOne), subNat(y.abs, natOne)
		return intOf(false, andNotNat(y1, x1))
	case x.neg:
		// (-x) &^ y == ^(x-1) &^ y == ^((x-1) | y) == -(((x-1) | y) + 1)
		x1 := subNat(x.abs, natOne)
		return intOf(true, addNat(orNat(x1, y.abs), natOne))
	}
	// x &^ (-y) == x &^ ^(y-1) == x & (y-1)
	return intOf(false, andNat(x.abs, subNat(y.abs, natOne)))
}

func (x bigInt) or(y bigInt) bigInt {
	switch {
	case !x.neg && !y.neg:
		return intOf(false, orNat(x.abs, y.abs))
	case x.neg && y.neg:
		// (-x) | (-y) == ^(x-1) | ^(y-1) == ^((x-1) & (y-1)) == -(((x-1) & (y-1)) + 1)
		x1, y1 := subNat(x.abs, natOne), subNat(y.abs, natOne)
		return intOf(true, addNat(andNat(x1, y1), natOne))
	}
	if x.neg {
		x, y = y, x
	}
	// x | (-y) == x | ^(y-1) == ^((y-1) &^ x) == -(^((y-1) &^ x) + 1)
	y1 := subNat(y.abs, natOne)
	return intOf(true, addNat(andNotNat(y1, x.abs), natOne))
}

func (x bigInt) xor(y bigInt) bigInt {
	switch {
	case !x.neg && !y.neg:
		return intOf(false, xorNat(x.abs, y.abs))
	case x.neg && y.neg:
		// (-x) ^ (-y) == ^(x-1) ^ ^(y-1) == (x-1) ^ (y-1)
		x1, y1 := subNat(x.abs, natOne), subNat(y.abs, natOne)
		return intOf(false, xorNat(x1, y1))
	}
	if x.neg {
		x, y = y, x
	}
	// x ^ (-y) == x ^ ^(y-1) == ^(x ^ (y-1)) == -((x ^ (y-1)) + 1)
	y1 := subNat(y.abs, natOne)
	return intOf(true, addNat(xorNat(x.abs, y1), natOne))
}

func (x bigInt) lsh(s uint) bigInt { return intOf(x.neg, shlNat(x.abs, s)) }

// rsh shifts right arithmetically: negative values round toward minus
// infinity.
func (x bigInt) rsh(s uint) bigInt {
	if x.neg {
		// (-x) >> s == ^(x-1) >> s == ^((x-1) >> s) == -(((x-1) >> s) + 1)
		t := shrNat(subNat(x.abs, natOne), s)
		return intOf(true, addNat(t, natOne))
	}
	return intOf(false, shrNat(x.abs, s))
}

func (x bigInt) String() string {
	if x.neg {
		return compat.Concat("-", x.abs.decimal())
	}
	return x.abs.decimal()
}

// scanSign consumes a leading sign of s.
func scanSign(s string) (neg bool, rest string, ok bool) {
	if s == "" {
		return false, s, false
	}
	switch s[0] {
	case '-':
		return true, strings.SliceFrom(s, 1), true
	case '+':
		return false, strings.SliceFrom(s, 1), true
	}
	return false, s, true
}

// natScan scans an unsigned number at the start of s as big's nat.scan
// does with base 0: a 0b, 0o or 0x prefix selects the base, and, if fracOk
// is false, a leading 0 selects octal; underscores may separate digits.
// With fracOk a radix point is accepted: count <= 0 then gives minus the
// number of digits after it. It returns the value, the base, the digit
// count, what follows the number, and whether the number is well-formed.
func natScan(s string, fracOk bool) (z nat, base, count int, rest string, ok bool) {
	prev := byte('.')
	invalSep := false
	base = 10
	prefix := byte(0)
	i := 0
	if i < len(s) && s[i] == '0' {
		prev = '0'
		count = 1
		i++
		if i < len(s) {
			switch s[i] {
			case 'b', 'B':
				base, prefix = 2, 'b'
			case 'o', 'O':
				base, prefix = 8, 'o'
			case 'x', 'X':
				base, prefix = 16, 'x'
			default:
				if !fracOk {
					base, prefix = 8, '0'
				}
			}
			if prefix != 0 {
				count = 0
				if prefix != '0' {
					i++
				}
			}
		}
	}
	dp := -1
	for ; i < len(s); i++ {
		ch := s[i]
		if ch == '.' && fracOk {
			fracOk = false
			if prev == '_' {
				invalSep = true
			}
			prev = '.'
			dp = count
			continue
		}
		if ch == '_' {
			if prev != '0' {
				invalSep = true
			}
			prev = '_'
			continue
		}
		var d uint64
		switch {
		case '0' <= ch && ch <= '9':
			d = uint64(ch - '0')
		case 'a' <= ch && ch <= 'z':
			d = uint64(ch-'a') + 10
		case 'A' <= ch && ch <= 'Z':
			d = uint64(ch-'A') + 10
		default:
			d = 1 << 32
		}
		if d >= uint64(base) {
			break
		}
		prev = '0'
		count++
		z = mulAddWW(z, uint64(base), d)
	}
	rest = strings.SliceFrom(s, i)
	ok = !invalSep && prev != '_'
	if count == 0 {
		if prefix == '0' {
			// Only the octal prefix 0: a decimal 0.
			return nil, 10, 1, rest, ok
		}
		ok = false
	}
	if dp >= 0 {
		count = dp - count
	}
	return z, base, count, rest, ok
}

// intFromString parses s as big.Int.SetString(s, 0) does.
func intFromString(s string) (bigInt, bool) {
	neg, s, ok := scanSign(s)
	if !ok {
		return bigInt{}, false
	}
	z, _, _, rest, ok := natScan(s, false)
	if !ok || rest != "" {
		return bigInt{}, false
	}
	return intOf(neg, z), true
}
