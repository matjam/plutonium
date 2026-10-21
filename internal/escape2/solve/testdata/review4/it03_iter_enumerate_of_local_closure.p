package main

import (
	"pluto/iter"
)

var gs iter.Seq2[int, int]

//pu:noinline
func f() {
	x := 42
	s := func(yield func(int) bool) { yield(x) }
	gs = iter.Enumerate(s)
}

func main() {
	f()
	println(clobber(1000))
	for _, v := range gs {
		println("value (42 if intact):", v)
	}
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

