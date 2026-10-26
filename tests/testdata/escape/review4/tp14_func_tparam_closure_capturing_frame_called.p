package main

import (
)

var keepF func() int

func keeper() int { return 0 }

func call[F ~func(func() int)](g F, h func() int) { g(h) }

func store(h func() int) { keepF = h }

//pu:noinline
func f() {
	x := 42
	call(store, func() int { return x })
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

