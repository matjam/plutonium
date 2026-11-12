package main

import "pluto/vec"

type H owner struct{ p *int }

func (h *H) Drop() {
	if h.p != nil {
		println(*h.p)
	}
}

func main() {
	var h H
	{
		var v vec.Vec[int]
		v.Push(7)
		h.p = v.Ptr(0)
		println(v.Len())
	}
	println("after")
}
