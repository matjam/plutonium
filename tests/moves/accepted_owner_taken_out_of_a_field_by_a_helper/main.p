package main

import "pluto/vec"

// take moves the value out and leaves the zero value: the result of a
// function that returns take(&q.v) is a new owner, not a view of q
// (language spec section 18.5).

type Q owner struct{ v vec.Vec[int] }

func takeV(q *Q) vec.Vec[int] { return take(&q.v) }

func (q *Q) TakeV() vec.Vec[int] { return take(&q.v) }

func main() {
	var q Q
	q.v.Push(1)
	w := takeV(&q)
	q.v.Push(2)
	println(w.Len())

	x := q.TakeV()
	q.v.Push(3)
	q.v.Push(4)
	println(x.Len())

	var r Q
	r.v.Push(5)
	f := func(q *Q) vec.Vec[int] { return take(&q.v) }
	y := f(&r)
	r.v.Push(6)
	println(y.Len())
}
