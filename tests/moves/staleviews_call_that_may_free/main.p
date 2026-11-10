package main

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/vec"
)

// A view of a local owner's memory ends at a call that may free that
// memory, or move an owner out of it, through a pointer to the owner
// (language spec section 18.3, A local owner's memory; 7.17; 11.24).

type Leaf owner struct{ x int }

type Node owner struct {
	child owned[Leaf]
	n     int
}

type H owner *Leaf

var leaf Leaf

func newH() H { return H(&leaf) }

func reset(p *owned[Leaf]) { *p = new(Leaf) } // drops the old box

func push(v *vec.Vec[int]) { v.Push(1) } // through a callee

func replace(n *Node) { n.child = new(Leaf) }

func grow(n *Node) { replace(n) } // two calls deep

type Pusher interface{ Push(x int) }

func pushVia(p Pusher) { p.Push(1) } // through the interface it is given

type Byter interface{ Bytes() []byte }

type Buf owner struct{ v vec.Vec[byte] }

func (b *Buf) Bytes() []byte { return b.v.Slice() }

func (b *Buf) Grow() { b.v.Push(0) }

func bytesOf(x Byter) []byte { return x.Bytes() } // a method result holds []byte

type Strs owner struct{ a [2]string }

// moveOut moves p.a[0] into *dst: with no RetainValues after it, the
// source no longer owns its values.
func moveOut(p *Strs, dst *string) { rt.MoveValues(dst, &p.a[0], 1) }

func main() {
	// A method that may free the old array.
	var v vec.Vec[int]
	v.Push(0)
	s := v.Slice()
	v.Push(1)
	println(s[0])

	// A function the pointer is passed to, which frees through a callee.
	var w vec.Vec[int]
	w.Push(0)
	t := w.Slice()
	push(&w)
	println(t[0])

	// A box replaced through a pointer to the variable.
	b := new(Leaf)
	q := &*b
	reset(&b)
	println(q.x)

	// An owner field replaced two calls deep.
	n := Node{child: new(Leaf)}
	c := &*n.child
	grow(&n)
	println(c.x)

	// A field of the variable's own storage is a view obtained from it,
	// not the variable itself.
	m := Node{child: new(Leaf)}
	k := &m.n
	grow(&m)
	println(*k)

	// A ring of owners: Pop moves the oldest element out.
	var r ring[Leaf, 4]
	r.Push(Leaf{x: 1})
	p, _ := r.Peek()
	if l, ok := r.Pop(); ok {
		l.Drop()
	}
	println(p.x)

	// Reset drops the elements.
	var r2 ring[Leaf, 4]
	r2.Push(Leaf{x: 1})
	p2, _ := r2.Peek()
	r2.Reset()
	println(p2.x)

	// An owner field on the view's path assigned directly.
	d := Node{child: new(Leaf)}
	e := &*d.child
	d.child = new(Leaf)
	println(e.x)

	// Another copy of a counted reference frees through the same object.
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(0)
	a2 := a
	sa := a.Slice()
	a2.Push(1)
	println(sa[0])

	// An ordinary interface parameter that holds the owner's address.
	var g vec.Vec[int]
	g.Push(0)
	gs := g.Slice()
	pushVia(&g)
	println(gs[0])

	// A result of a function given the owner as an ordinary interface,
	// whose methods may yield what the owner holds.
	var bb Buf
	bb.v.Push(1)
	bs := bytesOf(&bb)
	bb.Grow()
	println(bs[0])

	// MoveValues moves the values out of its source.
	var st Strs
	st.a[0] = "x"
	sp := &st.a[1]
	var out string
	moveOut(&st, &out)
	println(*sp)

	// The pointer result of Peek is stale after Reset; its bool is not.
	var r4 ring[Leaf, 4]
	r4.Push(Leaf{x: 1})
	p4, ok4 := r4.Peek()
	r4.Reset()
	println(ok4, p4.x)

	// (*Leaf)(h) looks at the object h owns.
	h := newH()
	hp := (*Leaf)(h)
	h = newH()
	println(hp.x)
}
