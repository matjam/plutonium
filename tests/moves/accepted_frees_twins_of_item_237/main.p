package main

import (
	"pluto/alloc"
	"pluto/vec"
)

// The twins of the item 237 rejections: calls that free nothing the views
// look at, owners that free nothing a stored view sees, and stored values
// loaded through a view that own nothing (language spec section 18.3).

type Holder struct{ r shared[vec.Vec[int]] }

func peek(h *Holder) int { return h.r.Len() }

type Lener interface{ Len() int }

type Box struct{ l Lener }

func size(b *Box) int { return b.l.Len() }

type Buf owner struct {
	b []int
	n int
}

type H struct{ s []int }

var keep H

func save(s []int) { keep.s = s }

type T struct{ n int }

var ga T
var arr [2]int
var kp *T

func savep(p *T) { kp = p }

// saveFirstp keeps a pointer element loaded through its argument, which
// points to memory the vector does not free.
func saveFirstp(s []*T) { kp = s[0] }

func main() {
	// A copy of a counted reference in a field that only reads.
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(0)
	h := Holder{r: a}
	sa := a.Slice()
	println(peek(&h))
	println(sa[0])

	// An interface in a field whose methods free nothing.
	var g vec.Vec[int]
	g.Push(0)
	gs := g.Slice()
	b := Box{l: &g}
	println(size(&b))
	println(gs[0])

	// An owner whose drop frees nothing its plain slice looks at.
	{
		o := Buf{b: arr[:]}
		save(o.b)
	}
	println(keep.s[0])

	// A stored value loaded through a view that owns nothing.
	ts := vec.New[*T]()
	ts.Push(&ga)
	savep(ts.Slice()[0])
	ts.Free()
	println(kp.n)

	us := vec.New[*T]()
	us.Push(&ga)
	saveFirstp(us.Slice())
	us.Free()
	println(kp.n)
}
