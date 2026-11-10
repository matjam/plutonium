package main

// An ordinary interface that a one-word owner is lent to holds a copy of
// the word, not the variable's address, so it is a view obtained from the
// variable: a call that may free what the word points to ends it (language
// spec section 18.3, A local owner's memory; 7.9). So is the result of an
// assertion from an owning interface to an ordinary interface (section
// 11.13). A type switch clause that names an ordinary interface moves
// nothing (section 12.9), which waits for item 148.

type Leaf owner struct{ x int }

func (l *Leaf) Len() int { return l.x }

type H owner *Leaf

var leaf Leaf

func newH() H { return H(&leaf) }

type Wrap owner struct{ b owned[Leaf] }

type Lener interface{ Len() int }

type Res owner interface{ Len() int }

type Big owner struct{ a, b, c int }

func (b *Big) Len() int { return b.a }

func reset(p *owned[Leaf]) { *p = new(Leaf) } // drops the old box

func renew(p *H) { *p = newH() }

func rewrap(w *Wrap) { w.b = new(Leaf) }

func refill(r *Res) { *r = new(Big) }

func main() {
	// A box.
	b := new(Leaf)
	x := any(b)
	reset(&b)
	println(x != nil)

	// An owner defined from a pointer type.
	h := newH()
	y := any(h)
	renew(&h)
	println(y != nil)

	// A one-field struct of a box.
	w := Wrap{b: new(Leaf)}
	z := any(w)
	rewrap(&w)
	println(z != nil)

	// An assertion from an owning interface to an ordinary one.
	var r Res = new(Big)
	l := r.(Lener)
	refill(&r)
	println(l.Len())

	// A function literal that holds such a view.
	b2 := new(Leaf)
	x2 := any(b2)
	show := func() { println(x2 != nil) }
	reset(&b2)
	show()
}
