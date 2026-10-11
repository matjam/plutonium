//pu:build kernel

package fmt

// In kernel mode floating point is unavailable outside //pu:fpu functions
// (language spec section 20.7), so a float argument prints as its bits in
// hexadecimal: %!v(float64=0x4020000000000000).
func (p *printer) printFloatBits(bits uint64, bitSize int, verb rune) {
	p.writeString("%!")
	p.writeRune(verb)
	if bitSize == 32 {
		p.writeString("(float32=0x")
	} else {
		p.writeString("(float64=0x")
	}
	p.fmtInteger(bits, false, 16, 'x', false)
	p.writeByte(')')
}
