// Translated by go2pu from internal/types2/packed_compare.go at the go-cutover tag; edited by hand since.

package types2

import (
	"selfhost/compat"
)

// ComparedBits returns the bits of p that take part in comparison and map
// hashing (spec section 7.14): every bit except the reserved bits, which are
// the bits of the `_` fields and the bits of the storage that no field
// covers. Bit position q is bit q%8 of byte q/8 of the result, whose length
// is the size of p in bytes; it is nil when no bit is reserved, and for a
// packed union, whose bits are all compared.
func (p *Packed) ComparedBits() []byte {
	if p.IsUnion() {
		return nil
	}
	mask := compat.Make[byte](int(p.bits / 8))
	p.markCompared(mask, 0)
	for _, b := range mask {
		if b != 0xFF {
			return mask
		}
	}
	return nil
}

// markCompared sets in mask, from bit position at, the bits of the fields of
// p that are not reserved.
func (p *Packed) markCompared(mask []byte, at int64) {
	for _, f := range p.fields {
		if f.obj.name == "_" {
			continue
		}
		markType(mask, f.obj.Type(), at+f.offset, f.width)
	}
}

// markType sets the bits of a field of type T and width bits at bit position
// at: all of them, except that a packed type (or an array of them) keeps its
// own reserved bits clear.
func markType(mask []byte, T Type, at, width int64) {
	switch u := T.Underlying().(type) {
	case *Packed:
		if !u.IsUnion() {
			u.markCompared(mask, at)
			return
		}
	case *Array:
		if _, ok := u.elem.Underlying().(*Packed); ok && u.len > 0 {
			for i := range u.len {
				markType(mask, u.elem, at+i*(width/u.len), width/u.len)
			}
			return
		}
	}
	for q := at; q < at+width; q++ {
		mask[q/8] |= 1 << (q % 8)
	}
}
