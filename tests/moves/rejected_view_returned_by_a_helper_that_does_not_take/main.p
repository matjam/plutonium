package main

import "pluto/vec"

// A helper that returns a slice of the field rather than taking it out
// returns a view of q's heap memory, which a push may free (language spec
// section 18.3).

type Q owner struct{ v vec.Vec[int] }

func viewV(q *Q) []int { return q.v.Slice() }

func main() {
	var q Q
	q.v.Push(1)
	w := viewV(&q)
	q.v.Push(2)
	println(w[0])
}
