package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// Float formatting: exact, using multiprecision decimal arithmetic, with the
// shortest representation that reads back to the same value when no
// precision is given. Adapted from Go's strconv/ftoa.go.

type floatInfo struct {
	mantbits uint
	expbits  uint
	bias     int
}

var float32info = floatInfo{23, 8, -127}
var float64info = floatInfo{52, 11, -1023}

// FormatFloat returns the text of f as AppendFloat formats it, a new string
// from the current allocator.
func FormatFloat(f float64, fmt byte, prec, bitSize int) string {
	// At most 330 digits before the point, plus the precision, exponent,
	// sign and point.
	if prec <= 400 {
		var buf [800]byte
		return string(AppendFloat(buf[:0], f, fmt, prec, bitSize))
	}
	n := 400 + prec
	p, ctl := rt.NewStringBuffer(n, alloc.Default())
	b := AppendFloat(unsafe.Slice(p, n)[:0], f, fmt, prec, bitSize)
	s := rt.MakeString(p, len(b), ctl, true)
	rt.ReleaseString(ctl)
	return s
}

// AppendFloat appends the text of f, formatted with fmt ('e', 'E', 'f', 'g',
// 'G', 'b', 'x' or 'X') and precision prec (-1 means the fewest digits that
// read back exactly). bitSize is 32 or 64.
func AppendFloat(dst []byte, f float64, fmt byte, prec, bitSize int) []byte {
	var bits uint64
	var flt *floatInfo
	switch bitSize {
	case 32:
		f32 := float32(f)
		bits = uint64(*(*uint32)(unsafe.Pointer(&f32)))
		flt = &float32info
	case 64:
		bits = *(*uint64)(unsafe.Pointer(&f))
		flt = &float64info
	default:
		panic("strconv: illegal AppendFloat bitSize")
	}

	neg := bits>>(flt.expbits+flt.mantbits) != 0
	exp := int(bits>>flt.mantbits) & (1<<flt.expbits - 1)
	mant := bits & (uint64(1)<<flt.mantbits - 1)

	switch exp {
	case 1<<flt.expbits - 1:
		switch {
		case mant != 0:
			return append(dst, "NaN"...)
		case neg:
			return append(dst, "-Inf"...)
		}
		return append(dst, "+Inf"...)
	case 0:
		exp++ // denormal
	default:
		mant |= uint64(1) << flt.mantbits
	}
	exp += flt.bias

	if fmt == 'b' {
		return fmtB(dst, neg, mant, exp-int(flt.mantbits))
	}
	if fmt == 'x' || fmt == 'X' {
		return fmtX(dst, prec, fmt, neg, mant, exp, flt)
	}

	var d decimal
	d.assign(mant)
	d.shift(exp - int(flt.mantbits))
	shortest := prec < 0
	if shortest {
		roundShortest(&d, mant, exp, flt)
		switch fmt {
		case 'e', 'E':
			prec = max(d.nd-1, 0)
		case 'f':
			prec = max(d.nd-d.dp, 0)
		case 'g', 'G':
			prec = d.nd
		}
	} else {
		switch fmt {
		case 'e', 'E':
			d.round(prec + 1)
		case 'f':
			d.round(d.dp + prec)
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			d.round(prec)
		}
	}
	return fmtEFG(dst, neg, d.d[:], d.dp, d.nd, prec, fmt, shortest)
}

// roundShortest rounds d (= mant * 2**(exp-mantbits)) to the fewest digits
// that still identify the float uniquely.
func roundShortest(d *decimal, mant uint64, exp int, flt *floatInfo) {
	if mant == 0 {
		d.nd = 0
		return
	}
	minexp := flt.bias + 1
	if exp > minexp && 332*(d.dp-d.nd) >= 100*(exp-int(flt.mantbits)) {
		return // already shortest
	}

	// upper is halfway between d and the next float up.
	var upper decimal
	upper.assign(mant*2 + 1)
	upper.shift(exp - int(flt.mantbits) - 1)

	// lower is halfway between d and the next float down.
	var mantlo uint64
	var explo int
	if mant > uint64(1)<<flt.mantbits || exp == minexp {
		mantlo = mant - 1
		explo = exp
	} else {
		mantlo = mant*2 - 1
		explo = exp - 1
	}
	var lower decimal
	lower.assign(mantlo*2 + 1)
	lower.shift(explo - int(flt.mantbits) - 1)

	// The bounds themselves are allowed only for an even mantissa, which
	// round-to-even maps back to this float.
	inclusive := mant%2 == 0

	// upperdelta tracks whether rounding up stays within the upper bound:
	// 0 if d and upper agree so far, 1 if they differ by exactly one unit
	// followed by 9s and 0s, 2 if rounding up is certainly within bounds.
	var upperdelta uint8
	for ui := 0; ; ui++ {
		mi := ui - upper.dp + d.dp
		if mi >= d.nd {
			break
		}
		li := ui - upper.dp + lower.dp
		l := byte('0')
		if li >= 0 && li < lower.nd {
			l = lower.d[li]
		}
		m := byte('0')
		if mi >= 0 {
			m = d.d[mi]
		}
		u := byte('0')
		if ui < upper.nd {
			u = upper.d[ui]
		}

		okdown := l != m || inclusive && li+1 == lower.nd

		switch {
		case upperdelta == 0 && m+1 < u:
			upperdelta = 2
		case upperdelta == 0 && m != u:
			upperdelta = 1
		case upperdelta == 1 && (m != '9' || u != '0'):
			upperdelta = 2
		}
		okup := upperdelta > 0 && (inclusive || upperdelta > 1 || ui+1 < upper.nd)

		switch {
		case okdown && okup:
			d.round(mi + 1)
			return
		case okdown:
			d.roundDown(mi + 1)
			return
		case okup:
			d.roundUp(mi + 1)
			return
		}
	}
}

// fmtEFG formats the digits s[:nd] with decimal point dp.
func fmtEFG(dst []byte, neg bool, s []byte, dp, nd, prec int, fmt byte, shortest bool) []byte {
	if fmt == 'g' || fmt == 'G' {
		eprec := prec
		if eprec > nd && nd >= dp {
			eprec = nd
		}
		if shortest {
			eprec = 6 // %e is used if the exponent is < -4 or >= 21 in Go's %v; 6 for %g
		}
		exp := dp - 1
		if exp < -4 || exp >= eprec {
			if prec > nd {
				prec = nd
			}
			prec--
			fmt = fmt + 'e' - 'g'
		} else {
			if prec > dp {
				prec = nd
			}
			prec = max(prec-dp, 0)
			fmt = 'f'
		}
	}

	switch fmt {
	case 'e', 'E':
		if neg {
			dst = append(dst, '-')
		}
		ch := byte('0')
		if nd != 0 {
			ch = s[0]
		}
		dst = append(dst, ch)
		if prec > 0 {
			dst = append(dst, '.')
			i := 1
			m := min(nd, prec+1)
			if i < m {
				dst = append(dst, s[i:m]...)
				i = m
			}
			for ; i <= prec; i++ {
				dst = append(dst, '0')
			}
		}
		dst = append(dst, fmt)
		exp := dp - 1
		if nd == 0 {
			exp = 0
		}
		if exp < 0 {
			ch = '-'
			exp = -exp
		} else {
			ch = '+'
		}
		dst = append(dst, ch)
		switch {
		case exp < 10:
			dst = append(dst, '0', byte(exp)+'0')
		case exp < 100:
			dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
		default:
			dst = append(dst, byte(exp/100)+'0', byte(exp/10)%10+'0', byte(exp%10)+'0')
		}
		return dst

	case 'f':
		if neg {
			dst = append(dst, '-')
		}
		if dp > 0 {
			m := min(nd, dp)
			dst = append(dst, s[:m]...)
			for ; m < dp; m++ {
				dst = append(dst, '0')
			}
		} else {
			dst = append(dst, '0')
		}
		if prec > 0 {
			dst = append(dst, '.')
			for i := 1; i <= prec; i++ {
				ch := byte('0')
				if j := dp + i - 1; 0 <= j && j < nd {
					ch = s[j]
				}
				dst = append(dst, ch)
			}
		}
		return dst
	}
	return append(dst, '%', fmt)
}

// fmtB formats mantissa and binary exponent: -ddddp±ddd.
func fmtB(dst []byte, neg bool, mant uint64, exp int) []byte {
	if neg {
		dst = append(dst, '-')
	}
	dst = AppendUint(dst, mant, 10)
	dst = append(dst, 'p')
	if exp >= 0 {
		dst = append(dst, '+')
	}
	return AppendInt(dst, int64(exp), 10)
}

// fmtX formats hexadecimal floating point: -0x1.yyyyp±dd.
func fmtX(dst []byte, prec int, fmt byte, neg bool, mant uint64, exp int, flt *floatInfo) []byte {
	if mant == 0 {
		exp = 0
	}
	mant <<= 60 - flt.mantbits
	for mant != 0 && mant&(1<<60) == 0 {
		mant <<= 1
		exp--
	}
	if prec >= 0 && prec < 15 {
		shift := uint(prec * 4)
		extra := (mant << shift) & (1<<60 - 1)
		mant >>= 60 - shift
		if extra|(mant&1) > 1<<59 {
			mant++
		}
		mant <<= 60 - shift
		if mant&(1<<61) != 0 {
			mant >>= 1
			exp++
		}
	}
	hex := "0123456789abcdef"
	if fmt == 'X' {
		hex = "0123456789ABCDEF"
	}
	if neg {
		dst = append(dst, '-')
	}
	dst = append(dst, '0', fmt, '0'+byte((mant>>60)&1))
	mant <<= 4
	if prec < 0 && mant != 0 {
		dst = append(dst, '.')
		for mant != 0 {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	} else if prec > 0 {
		dst = append(dst, '.')
		for i := 0; i < prec; i++ {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	}
	ch := byte('P')
	if fmt == 'x' {
		ch = 'p'
	}
	dst = append(dst, ch)
	if exp < 0 {
		ch = '-'
		exp = -exp
	} else {
		ch = '+'
	}
	dst = append(dst, ch)
	switch {
	case exp < 100:
		dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
	case exp < 1000:
		dst = append(dst, byte(exp/100)+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	default:
		dst = append(dst, byte(exp/1000)+'0', byte(exp/100)%10+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	}
	return dst
}
