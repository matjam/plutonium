package main

import "pluto/vec"

// A view of the owner stored through a method result is used while the
// owner and its memory live, and nothing frees that memory: accepted.

type H struct{ s []int }

var g H

type O owner struct {
	v   vec.Vec[int]
	ext *H
}

func (o *O) Ext() *H { return o.ext }

func main() {
	o := O{v: vec.New[int](), ext: &g}
	o.v.Push(1)
	h := o.Ext()
	h.s = o.v.Slice()
	println(g.s[0])
	h.s = nil
	println(o.v.Len())
}
