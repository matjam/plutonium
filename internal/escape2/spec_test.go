package escape2

import (
	"fmt"
	"slices"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
)

// lineSites returns the errors of err as "line:col code", in order.
func lineSites(err error) []string {
	var out []string
	add := func(d *diag.Diagnostic) {
		out = append(out, fmt.Sprintf("%d:%d %s", d.Pos.Line, d.Pos.Column, d.Code))
	}
	switch e := err.(type) {
	case *diag.List:
		for _, d := range *e {
			add(d)
		}
	case *diag.Diagnostic:
		add(e)
	}
	return out
}

// checkSpec checks that the analysis reports exactly want on src: the
// errors the spec section a case names requires, and no others.
func checkSpec(t *testing.T, name, src string, want ...string) {
	t.Helper()
	err := Check(loadSrc(t, src))
	if err != nil && isInternal(err) {
		t.Fatalf("%s: %v", name, err)
	}
	if got := lineSites(err); !slices.Equal(got, want) {
		t.Errorf("%s: errors %v, want %v\n%v", name, got, want, err)
	}
}

// A view of an owner temporary that a pointer method is called on may be
// passed to calls in its statement and may not be kept (section 11.14,
// Calls; section 18.3, Views; section 18.6, Drops), whether or not the
// owner type has anything to drop and whether or not the variable that
// keeps it is read again.
func TestOwnerTempViewKept(t *testing.T) {
	for _, drop := range []string{"", "func (b *Buf) Drop() {}\n"} {
		src := `package main

type Buf owner struct {
	data [4]int
	n    int
}

func (b *Buf) Slice() []int { return b.data[:b.n] }

func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}

func sum(s []int) int { return len(s) }

func main() {
	println(sum(readAll(3).Slice()))
	if s := readAll(3).Slice(); len(s) > 0 {
		println(s[0])
	}
	s := readAll(3).Slice()
	println(len(s))
	u := readAll(3).Slice()
	_ = u
	for v := readAll(2).Slice(); len(v) > 0; v = v[1:] {
		println(v[0])
	}
}
` + drop
		name := "no Drop"
		if drop != "" {
			name = "Drop"
		}
		checkSpec(t, name, src, "23:2 E3009", "25:2 E3009", "27:6 E3009")
	}
}
