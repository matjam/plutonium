package main

import (
)

type S struct{ r *[]int; s []int }

func link(s *S) { *s.r = s.s }

var keepS []int

//pu:noinline
func f() {
	a := [2]int{42, 43}
	var out []int
	s := S{&out, a[:]}
	link(&s)
	keepS = out
}

func main() {
	f()
	println(clobber(1000))
	println("slice (42 if intact):", keepS[0])
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

