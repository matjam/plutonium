package main

import "pluto/vec"

// A method of a local owner returns heap memory the owner does not own.
// Storing a view of the owner there keeps it past the owner.

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
	o.v.Free()
	println(g.s[0])
}
