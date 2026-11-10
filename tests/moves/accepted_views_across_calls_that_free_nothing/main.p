package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/rt"
	"pluto/vec"
)

// Calls that free nothing through their pointer arguments leave the views
// of the owner alone, and a view of the variable itself (&v) survives a
// call that frees what it holds (language spec section 18.3).

type Leaf owner struct{ x int }

type Node owner struct {
	child owned[Leaf]
	n     int
}

func look(v *vec.Vec[int]) int { return v.Len() }

type Strs owner struct{ a [2]string }

// copyOut copies p.a[0] into *dst: RetainValues of the same destination
// and count right after the MoveValues makes the pair a copy, so the
// source keeps its values.
func copyOut(p *Strs, dst *string) {
	rt.MoveValues(dst, &p.a[0], 1)
	rt.RetainValues(dst, 1)
}

// keepBytes copies what b holds into a new vector, as os.NewNameError
// does: Append copies its variadic values.
func keepBytes(b *Buf) int {
	var c vec.Vec[byte]
	c.Append(b.v.Slice()...)
	n := c.Len()
	c.Drop()
	return n
}

func bump(n *Node) { n.n++ }

func reset(p *owned[Leaf]) { *p = new(Leaf) }

func other(p *owned[Leaf]) { *p = new(Leaf) }

type H owner *Leaf

type Reader interface{ Read(p []byte) int }

type Buf owner struct{ v vec.Vec[byte] }

func (b *Buf) Read(p []byte) int { return copy(p, b.v.Slice()) }

func (b *Buf) Grow() { b.v.Push(0) }

var store [8]byte

// readAll reaches b only through Read, whose results hold no slice.
func readAll(r Reader) []byte {
	n := r.Read(store[:])
	return store[:n]
}

var leaf Leaf

func newH() H { return H(&leaf) }

func main() {
	var v vec.Vec[int]
	v.Push(0)
	s := v.Slice()
	println(look(&v), v.Len(), s[0])
	println(s[0])

	n := Node{child: new(Leaf)}
	c := &*n.child
	bump(&n)
	println(c.x)

	// &v is the variable itself: it sees the new value.
	b := new(Leaf)
	pb := &b
	reset(&b)
	println((*pb).x)

	// A view used before the call, and made again after it.
	var w vec.Vec[int]
	w.Push(0)
	t := w.Slice()
	println(t[0])
	w.Push(1)
	t = w.Slice()
	println(t[1])

	// The call frees through another variable.
	x := new(Leaf)
	y := new(Leaf)
	qx := &*x
	other(&y)
	println(qx.x)

	// A field the view does not look through.
	d := Node{child: new(Leaf)}
	e := &*d.child
	d.n = 2
	println(e.x)

	// Copies of a counted reference that only read.
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(0)
	a2 := a
	sa := a.Slice()
	println(a2.Len(), sa[0])

	// An arena's allocations free nothing of the arena's memory, though
	// its parent allocator, an ordinary interface, may free its own.
	ar := arena.NewChunked()
	ax := ar.New[int]()
	ay := ar.Slice[int](64)
	*ax = len(ay)
	println(*ax)

	// A function given the owner as an ordinary interface whose methods
	// yield nothing the owner holds: its result is not a view of the owner
	// (section 7.9).
	var bb Buf
	bb.v.Push(1)
	rs := readAll(&bb)
	bb.Grow()
	println(len(rs))

	// A copy of the values (MoveValues, then RetainValues) frees nothing
	// of the source.
	var st Strs
	st.a[0] = "x"
	sp := &st.a[1]
	var out string
	copyOut(&st, &out)
	println(*sp, out)

	// Append copies the bytes of the view it is given.
	var kb Buf
	kb.v.Push(1)
	ks := kb.v.Slice()
	println(keepBytes(&kb), ks[0])

	// A result that cannot hold a view, as the bool of a Peek, is not
	// stale when the views of the others are.
	var r3 ring[Leaf, 4]
	r3.Push(Leaf{x: 1})
	p3, ok3 := r3.Peek()
	println(p3.x)
	r3.Reset()
	println(ok3)

	// (*Leaf)(h) used before h gets a new value.
	h := newH()
	hp := (*Leaf)(h)
	println(hp.x)
	h = newH()
}
