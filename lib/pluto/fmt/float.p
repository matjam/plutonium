//pu:build !kernel

package fmt

import (
	"pluto/strconv"
	"pluto/vec"
	"unsafe"
)

// Float formatting. Kernel builds, which may not use the FPU, get the
// stand-in in float_kernel.p instead.

// printFloatBits formats a float given by its bits: a float64, or a
// float32 in the low 32 bits when bitSize is 32.
func (p *printer) printFloatBits(bits uint64, bitSize int, verb rune) {
	f := *(*float64)(unsafe.Pointer(&bits))
	if bitSize == 32 {
		w := uint32(bits)
		f = float64(*(*float32)(unsafe.Pointer(&w)))
	}
	p.printFloat(f, bitSize, verb)
}

func (p *printer) printFloat(f float64, bitSize int, verb rune) {
	var buf [800]byte
	prec := -1
	if p.hasPrec {
		prec = p.prec
	}
	var fmtc byte
	switch verb {
	case 'v':
		fmtc = 'g'
	case 'e', 'E', 'f', 'g', 'G', 'x', 'X', 'b':
		fmtc = byte(verb)
	case 'F':
		fmtc = 'f'
	default:
		p.badVerb(verb)
		return
	}
	if (fmtc == 'e' || fmtc == 'E' || fmtc == 'f') && prec < 0 {
		prec = 6
	}
	// buf[0] is room for a sign the flags add.
	fb := vec.NewFixed(buf[1:])
	strconv.AppendFloat(&fb, f, fmtc, prec, bitSize)
	out := buf[:1+fb.Len()]
	// Sign handling: AppendFloat writes '-' for negatives; add + or space.
	start := 1
	if out[1] != '-' && out[1] != '+' {
		switch {
		case p.plus:
			out[0] = '+'
			start = 0
		case p.space:
			out[0] = ' '
			start = 0
		}
	} else if out[1] == '+' && !p.plus {
		start = 2 // +Inf without the + flag
	}
	body := out[start:]
	if p.zero && p.hasWid && !p.minus && len(body) < p.wid && body[len(body)-1] != 'f' && body[len(body)-1] != 'N' {
		// Zero padding goes after the sign.
		signLen := 0
		if body[0] == '-' || body[0] == '+' || body[0] == ' ' {
			signLen = 1
		}
		var padded [900]byte
		k := copy(padded[:], body[:signLen])
		for z := 0; z < p.wid-len(body); z++ {
			padded[k] = '0'
			k++
		}
		k += copy(padded[k:], body[signLen:])
		p.writeBytes(padded[:k])
		return
	}
	p.fieldNoZero(body)
}
