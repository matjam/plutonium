// Ported from internal/escape2/pointerfree_test.go at the go-cutover tag.

package escape2

import (
	"compiler/compat"
	"pluto/testing"
)

// pointerFreeSrc grows a slice into heap memory, as compat.Append's grow
// does, through a generic function whose constraint is the first operand. g reaches the
// slice through more loads than a summary follows, so its summary keeps
// everything reachable from its input (section 18.3), and main's frame
// objects that a reaches are kept unless grow's copy stores no pointers.
const pointerFreeSrc = `package main

import (
	"pluto/alloc"
	"unsafe"
)

type plain interface{ ~byte | ~int | ~bool }

type named byte

type viaNamed interface{ plain }

func grow[T %s](s []T) []T {
	var zero T
	n := 2*len(s) + 1
	p := alloc.Raw(uintptr(n)*unsafe.Sizeof(zero), unsafe.Alignof(zero))
	ns := unsafe.Slice((*T)(p), n)
	copy(ns, s)
	return ns
}

type A struct{ p *B }
type B struct{ q *[]%s }

func g(a **A) {
	s := (*a).p.q
	*s = grow(*s)
}

func main() {
	var buf [4]%s
	b := buf[:]
	y := B{q: &b}
	x := A{p: &y}
	pa := &x
	g(&pa)
}
`

// TestPointerFreeTypeParam: a type parameter whose constraint allows only
// types that hold no pointers holds none, so the generic body's copy into
// heap memory keeps nothing, whatever reaches the call. With a constraint
// that allows pointers, the same program is rejected.
func TestPointerFreeTypeParam(t *testing.T) {
	for _, c := range []struct {
		constraint, elem string
		rejected         bool
	}{
		{"plain", "byte", false},
		{"~byte", "named", false},
		{"viaNamed", "int", false},
		{"interface{ plain; comparable }", "bool", false},
		{"any", "byte", true},
		{"~byte | ~string", "byte", true},
		{"any", "*int", true},
	} {
		src := compat.Sprintf(pointerFreeSrc, c.constraint, c.elem, c.elem)
		err := Check(loadSrc(t, src))
		if isInternal(err) && err != nil {
			t.Fatalf("%s: %v", c.constraint, err)
		}
		got := len(sites(err)) > 0
		if got != c.rejected {
			t.Errorf("grow[T %s] of []%s: rejected %v, want %v (%v)", c.constraint, c.elem, got, c.rejected, err)
		}
	}
}
