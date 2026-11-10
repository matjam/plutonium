package main

// Views of locals with drop glue used within the local's scope, and views
// of locals without drop glue, which only the frame bounds (language spec
// section 18.3, A local's scope). The rejected twin is
// tests/diag/stale_scope_views.

import "pluto/strconv"

type Named struct {
	name string
	n    int
}

func (x *Named) Show() int { return len(x.name) + x.n }

type Pair struct{ a, b int }

func text(i int) string { return strconv.Itoa(i) }
func ok() bool          { return true }

type Out struct {
	s string
	n int
}

func fill(o *Out, i int) int { o.n = i; return i }

// runOn calls f with o: what it keeps of o is what the function its
// caller passes for f keeps.
func runOn(f func(*Out, int) int, i int, o *Out) { o.n = f(o, i) }

func main() {
	// The address of a counted string, used inside its block.
	var p *string
	{
		s := text(12345)
		p = &s
		println(*p)
	}

	// The address of an int declared in a block: bits only.
	var pi *int
	{
		n := 7
		pi = &n
	}
	println(*pi)

	// A struct without drop glue.
	var pp *Pair
	{
		x := Pair{a: 1, b: 2}
		pp = &x
	}
	println(pp.a + pp.b)

	// A string of the function's top block lasts as long as the frame.
	top := text(8)
	pt := &top
	{
		println(*pt)
	}
	println(*pt)

	// A method value used inside the block.
	{
		x := Named{name: text(2)}
		show := x.Show
		println(show())
	}

	// A loop body left by break, the view used before it.
	for i := 0; i < 3; i++ {
		s := text(i)
		q := &s
		println(*q)
		break
	}

	// A loop that leaves an int's view by break.
	var qi *int
	for i := 0; i < 3; i++ {
		n := i
		qi = &n
		break
	}
	println(*qi)

	// A view given a new value after the scope ended.
	var r *string
	{
		s := text(3)
		r = &s
		println(*r)
	}
	r = &top
	println(*r)

	// A view passed with a declared function that keeps nothing of it, as
	// the function value a callee calls it with.
	for i := 0; i < 2; i++ {
		var o Out
		runOn(fill, i, &o)
		println(o.n)
	}

	// A map in an if statement's init, used inside it.
	if m := map[int]int{1: 2}; ok() {
		pm := &m
		println(len(*pm))
	}
}
