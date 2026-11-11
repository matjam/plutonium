package main

import "pluto/vec"

// A Drop method's receiver points to the owner being dropped, whose
// memory is released when Drop returns (language spec section 18.6):
// keeping it, or a part of it, in package memory is E3002, wherever the
// owner lives.

type O owner struct{ n int }

var keep *O

func (o *O) Drop() { keep = o }

type P owner struct{ n int }

var keepN *int

func (p *P) Drop() { keepN = &p.n }

func main() {
	v := vec.New[O]()
	v.Push(O{n: 1})
	var p P
	p.n = 2
	println(v.Len(), p.n)
}
