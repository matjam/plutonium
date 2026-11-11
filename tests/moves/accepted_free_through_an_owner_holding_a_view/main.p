package main

import "pluto/vec"

// A call given an owner's address that frees nothing through it leaves
// alone the views of a local the owner holds a view of, and views made
// after the free are fine (sections 18.3 and 18.13).

type W owner struct{ p *vec.Vec[int] }

func look(w *W) int { return w.p.Len() }

func grow(w *W) { w.p.Push(1) }

func main() {
	y := vec.New[int]()
	y.Push(0)
	s := y.Slice()
	w := W{p: &y}
	println(look(&w))
	println(s[0])
	grow(&w)
	t := y.Slice()
	println(t[0])
}
