package main

import "pluto/arena"

// Reset of a local arena reclaims every allocation, and Release(m) those
// made after the Mark that gave m its value (spec 18.9): views of them are
// stale.

func main() {
	a := arena.NewFixed(64)
	p := a.New[int]()
	a.Reset()
	*p = 1

	b := arena.NewChunked()
	m := b.Mark()
	q := b.New[int]()
	b.Release(m)
	*q = 1

	c := arena.NewFixed(256)
	s := c.Slice[int](4)
	c.Reset()
	t := c.Slice[int](4)
	t[0] = 7
	println(s[0])

	// Fine: made before the mark, so Release keeps it, and made again
	// after Reset.
	d := arena.NewChunked()
	x := d.New[int]()
	dm := d.Mark()
	y := d.New[int]()
	*y = 2
	d.Release(dm)
	*x = 1
	d.Reset()
	z := d.New[int]()
	*z = 3
	println(*z)

	// Fine: a mark is a fill level, not memory of the arena. Release of a
	// stale mark is checked when it runs.
	e := arena.NewChunked()
	em := e.Mark()
	e.Reset()
	_ = em
}
