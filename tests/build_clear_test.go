// Ported from internal/build/clear_test.go at the go-cutover tag.

package tests

// Tests for clear of an owner type (language spec section 13.3): each element
// is dropped in index order, its Drop method runs before its place becomes the
// zero value, and nothing is dropped twice. The array is cleared as a slice,
// and when main ends the zeroed array is dropped again, printing zeros.

import (
	"testing"
)

func TestClearOwners(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/fmt"

type Box owner struct {
	id int
}

func (b *Box) Drop() { fmt.Println(b.id) }

func main() {
	var xs [3]Box
	xs[0] = Box{1}
	xs[1] = Box{2}
	xs[2] = Box{3}
	clear(xs[:])
	fmt.Println("cleared")
}`
	for _, opt := range []int{0, 2} {
		prog := buildConc(t, src, optFlag(opt))
		out, status := runExeEnv(t, prog)
		if status != 0 || out != "1\n2\n3\ncleared\n0\n0\n0\n" {
			t.Fatalf("-O%d: status %d, output %q; want the ids dropped in order, then the zeroed array dropped at the end", opt, status, out)
		}
	}
}

// A deferred clear of a slice of owners runs when the function returns,
// dropping each element in index order before it becomes the zero value
// (spec 12.19, 13.3). The caller's copy of the array is then dropped, zeroed,
// when the call ends.
func TestClearOwnersDeferred(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/fmt"

type Box owner struct {
	id int
}

func (b *Box) Drop() { fmt.Println(b.id) }

func f(xs [2]Box) {
	defer clear(xs[:])
	fmt.Println("in f")
}

func main() {
	f([2]Box{{1}, {2}})
	fmt.Println("after f")
}`
	want := "in f\n1\n2\n0\n0\nafter f\n"
	for _, opt := range []int{0, 2} {
		prog := buildConc(t, src, optFlag(opt))
		out, status := runExeEnv(t, prog)
		if status != 0 || out != want {
			t.Fatalf("-O%d: status %d, output %q; want %q", opt, status, out, want)
		}
	}
}

// Elements beyond len(s) are not changed, an element that is an owner with owner
// fields drops Drop first and then its fields, a slice that is nil or empty
// does nothing, and a type parameter whose type set is slices clears like the
// slice (spec 13.3).
func TestClearOwnersMore(t *testing.T) {
	t.Parallel()
	src := `package main

import "pluto/fmt"

type Inner owner struct {
	id int
}

func (b *Inner) Drop() { fmt.Println("inner", b.id) }

type Outer owner struct {
	id int
	in Inner
}

func (o *Outer) Drop() { fmt.Println("outer", o.id) }

func clearAll[S ~[]E, E owner any](s S) { clear(s) }

func main() {
	var xs [3]Outer
	xs[0] = Outer{1, Inner{10}}
	xs[1] = Outer{2, Inner{20}}
	xs[2] = Outer{3, Inner{30}}
	clear(xs[:2])
	fmt.Println("partial")
	var none []Outer
	clear(none)
	clear(xs[:0])
	fmt.Println("empty")
	clearAll(xs[2:])
	fmt.Println("generic")
}`
	want := "outer 1\ninner 10\nouter 2\ninner 20\npartial\nempty\nouter 3\ninner 30\ngeneric\n" +
		"outer 0\ninner 0\nouter 0\ninner 0\nouter 0\ninner 0\n"
	for _, opt := range []int{0, 2} {
		prog := buildConc(t, src, optFlag(opt))
		out, status := runExeEnv(t, prog)
		if status != 0 || out != want {
			t.Fatalf("-O%d: status %d, output %q; want %q", opt, status, out, want)
		}
	}
}
