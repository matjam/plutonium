package main

import (
)

type S struct{ r *func() int; g func() int }

func link(s *S) { *s.r = s.g }

var keepF func() int

//pu:noinline
func f() {
	x := 42
	var out func() int
	s := S{&out, func() int { return x }}
	link(&s)
	keepF = out
}

func main() {
	f()
	println(clobber(1000))
	println("closure (42 if intact):", keepF())
}

//pu:noinline
func clobber(n int) int {
	var a [64]int
	for i := range a {
		a[i] = n + i
	}
	s := 0
	for i := range a {
		s += a[i]
	}
	return s
}

