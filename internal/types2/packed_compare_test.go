package types2_test

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/types2"
)

// TestPackedComparedBits checks which bits of a packed type comparison looks
// at (spec section 7.14): not the bits of `_` fields and not the bits that
// no field covers; a nested packed type keeps its own reserved bits out too.
func TestPackedComparedBits(t *testing.T) {
	pkg, _ := mustCheck(t, `package p

type Full packed uint8 { a bits[uint, 4]; b bits[uint, 4] }
type Reg packed uint32 { mode bits[uint, 4]; _ bits[uint, 4]; on bits[bool, 1] }
type Hdr packed [3]byte { a bits[uint8, 3]; _ bits[uint8, 5]; b uint8 }
type Inner packed uint8 { v bits[uint, 2]; _ bits[uint, 6] }
type Outer packed [3]byte { i Inner; j [2]Inner }
type Flags packed uint8 { f bits { A; _; C } }
type U packed union uint8 { tag bits[uint, 2]; X case 0 { v bits[uint, 4] }; Y case 1 {} }
`)
	for _, c := range []struct {
		name string
		want []byte // nil: every bit is compared
	}{
		{"Full", nil},
		{"Reg", []byte{0x0F, 0x01, 0, 0}},
		{"Hdr", []byte{0x07, 0xFF, 0}},
		{"Outer", []byte{3, 3, 3}},
		{"Flags", []byte{0x05}},
		{"U", nil},
	} {
		p, ok := pkg.Scope().Lookup(c.name).Type().Underlying().(*types2.Packed)
		if !ok {
			t.Fatalf("%s is not a packed type", c.name)
		}
		got := p.ComparedBits()
		if !bytes.Equal(got, c.want) {
			t.Errorf("ComparedBits(%s) = %x, want %x", c.name, got, c.want)
		}
	}
}
