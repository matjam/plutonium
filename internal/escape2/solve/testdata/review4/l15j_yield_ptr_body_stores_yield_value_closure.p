package main

import (
)

type S struct{ y func(func() int) bool }

func run(s *S) {
	x := 42
	s.y(func() int { return x })
}

func seq(yield func(func() int) bool) {
	s := S{y: yield}
	run(&s)
}

var keepF func() int

//pu:noinline
func f() {
	for g := range seq {
		keepF = g
	}
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

