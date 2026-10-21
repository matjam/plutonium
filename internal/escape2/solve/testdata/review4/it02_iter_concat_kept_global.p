package main

import (
	"pluto/iter"
)

func one(yield func(int) bool) { yield(42) }

var gs iter.Seq[int]

//pu:noinline
func f() { gs = iter.Concat(one, one) }

func main() {
	f()
	println(clobber(1000))
	for v := range gs {
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

