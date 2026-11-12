package main

import "pluto/arena"

type S owner struct {
	n int
	a arena.Fixed
	b arena.Fixed
}

func main() {
	a := arena.NewFixed(4096)
	pa := &a
	p := pa.New[int]()
	*p = 7
	println(*p)
	a.Reset()
	s := S{a: arena.NewFixed(4096), b: arena.NewFixed(4096)}
	q := s.a.New[int]()
	*q = 8
	s.b.Reset()
	println(*q)
	s.a.Reset()
	r := s.a.New[int]()
	*r = 9
	println(*r)
}
