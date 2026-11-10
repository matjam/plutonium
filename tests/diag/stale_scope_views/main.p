package main

// Views of locals with drop glue used after the local's scope ended
// (language spec section 18.3, A local's scope; section 18.4 for leaving
// a loop). The accepted twin is tests/moves/accepted_views_within_their_scope.

import "pluto/strconv"

type Named struct {
	name string
	n    int
}

func (x *Named) Show() int { return len(x.name) + x.n }

func text(i int) string { return strconv.Itoa(i) }
func ok() bool          { return true }

type Out struct {
	s string
	n int
}

var kept *Out

func keep(o *Out, i int) int { kept = o; return i }

func runOn(f func(*Out, int) int, i int, o *Out) { o.n = f(o, i) }

func main() {
	// The address of a counted string declared in a block.
	var p *string
	{
		s := text(12345)
		p = &s
	}
	println(*p)

	// A slice of a local array of strings.
	var names []string
	{
		var arr [2]string
		arr[0] = text(1)
		names = arr[:]
	}
	println(names[0])

	// A method value of a struct holding a string.
	var show func() int
	{
		x := Named{name: text(2)}
		show = x.Show
	}
	println(show())

	// The address of a map declared in an if statement's init.
	var pm *map[int]int
	if m := map[int]int{1: 2}; ok() {
		pm = &m
	}
	println(len(*pm))

	// A loop body left by break.
	var q *string
	for i := 0; i < 3; i++ {
		s := text(i)
		q = &s
		break
	}
	println(*q)

	// A range variable, after the loop is left by break.
	var r *string
	for _, s := range []string{text(3), text(4)} {
		r = &s
		break
	}
	println(*r)

	// A case clause's variable.
	var c *string
	switch i := 5; i {
	case 5:
		s := text(i)
		c = &s
	}
	println(*c)

	// A view stored where the compiler does not follow it.
	var w *string
	pw := &w
	{
		s := text(6)
		*pw = &s
	}
	println(*w)

	// A view passed with a function that keeps it, as the function value
	// a callee calls it with.
	{
		var o Out
		runOn(keep, 1, &o)
	}
}
