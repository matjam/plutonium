package main

// An owner larger than one word lent to an ordinary interface is a view of
// the variable itself, so a call that frees what it holds leaves the
// interface alone: it points at the variable, which holds the new value
// (language spec section 18.3, A local owner's memory; 7.9). A one-word
// owner lent to an interface, or an assertion from an owning interface,
// may be used until such a call.

type Leaf owner struct{ x int }

type Pair owner struct {
	b owned[Leaf]
	n int
}

func (p *Pair) Len() int { return p.n }

type Lener interface{ Len() int }

type Res owner interface{ Len() int }

type Big owner struct{ a, b, c int }

func (b *Big) Len() int { return b.a }

func reset(p *owned[Leaf]) { *p = new(Leaf) }

func refresh(p *Pair) { p.b = new(Leaf) }

func refill(r *Res) { *r = new(Big) }

func look(r *Res) int { return (*r).Len() }

func main() {
	// A two-word owner: the interface points at p.
	p := Pair{b: new(Leaf), n: 1}
	x := Lener(&p)
	y := any(p)
	refresh(&p)
	println(x.Len(), y != nil)

	// A one-word owner used before the call.
	b := new(Leaf)
	z := any(b)
	println(z != nil)
	reset(&b)

	// An assertion used before the call, and a call that frees nothing.
	var r Res = new(Big)
	l := r.(Lener)
	println(look(&r), l.Len())
	refill(&r)
	l = r.(Lener)
	println(l.Len())
}
