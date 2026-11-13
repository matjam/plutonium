package main

import "pluto/vec"

// The view points into the object of element 1 of s. After s is resliced,
// s[0] is that element: its new value drops the box the view points into
// (language spec sections 18.3, 18.6).

type L owner struct{ x int }

func main() {
	var v vec.Vec[owned[L]]
	v.Push(new(L))
	v.Push(new(L))
	s := v.Slice()
	q := &s[1].x
	s = s[1:]
	s[0] = new(L)
	println(*q)
}
