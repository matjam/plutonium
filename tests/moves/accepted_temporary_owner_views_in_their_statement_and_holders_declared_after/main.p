package main

import (
	"pluto/arena"
	"pluto/vec"
)

func mk() vec.Vec[int] {
	var v vec.Vec[int]
	v.Push(7)
	v.Push(8)
	return v
}

func sum(s []int) int {
	t := 0
	for _, x := range s {
		t += x
	}
	return t
}

type H owner struct{ p *int }

func (h *H) Drop() {
	if h.p != nil {
		println("drop", *h.p)
	}
}

// Views of a temporary owner used within its statement, or within the if,
// switch or range statement whose header makes it (spec section 18.6), and
// holders declared after the owners they view (section 18.3).
func main() {
	println(sum(mk().Slice()))
	println(*mk().Ptr(0))
	x := *mk().Ptr(1)
	println(x)
	if s := mk().Slice(); len(s) > 1 {
		println(s[1])
	}
	switch p := mk().Ptr(0); *p {
	case 7:
		println("seven")
	}
	for _, y := range mk().Slice() {
		println(y)
	}

	var v vec.Vec[int]
	v.Push(9)
	var h H
	h.p = v.Ptr(0)

	// A vector of pointers frees only its buffer when it is dropped, so
	// it may be declared before what its elements point into.
	var out vec.Vec[*int]
	var w vec.Vec[int]
	w.Push(10)
	out.Push(w.Ptr(0))
	println(*out.At(0))

	// A holder declared first that no longer holds the view when the
	// owner is dropped.
	var g H
	{
		var u vec.Vec[int]
		u.Push(11)
		g.p = u.Ptr(0)
		println(*g.p)
		g.p = nil
	}

	a := arena.NewFixed(4096)
	av := vec.New[int](&a)
	av.Push(12)
	println(av.Len())
}
