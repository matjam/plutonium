package main

import "pluto/arena"

type S owner struct {
	n int
	a arena.Fixed
}

func main() {
	s := S{a: arena.NewFixed(4096)}
	p := s.a.New[int]()
	*p = 7
	s.a.Reset()
	println(*p)
}
