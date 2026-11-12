package main

import "pluto/vec"

// Idioms the view rules accept (language spec sections 18.3, 18.5):
// pushes through &v, take of a field, a take method of an owned box,
// Pop and Remove, a deferred call whose argument reads a view, Set of a
// box element, and calls on a view of a temporary within its statement.

type L owner struct{ x int }

type Q owner struct{ v vec.Vec[int] }

type Box owner struct{ c owned[L] }

func (b *Box) Take() owned[L] { return take(&b.c) }

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(9)
	return v
}

func main() {
	var v vec.Vec[int]
	p := &v
	p.Push(1)
	p.Push(2)
	println(p.Len())

	var q Q
	q.v.Push(1)
	w := take(&q.v)
	q.v.Push(2)
	println(w.Len())

	var bx Box
	bx.c = new(L)
	bx.c.x = 3
	l := bx.Take()
	bx.c = new(L)
	println(l.x)

	x, ok := v.Pop()
	println(x, ok)
	y := v.Remove(0)
	println(y, v.Len())

	v.Push(4)
	e := v.Ptr(0)
	defer println(*e)
	v.Push(5)

	var bs vec.Vec[owned[L]]
	bs.Push(new(L))
	bs.Set(0, new(L))
	println(bs.Slice()[0].x)

	println(mk().Len(), mk().Slice()[0])
}
