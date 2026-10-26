package main

import (
	"pluto/rt"
)

var keepS string

//pu:noinline
func g() string {
	b := [8]byte{'h', 'e', 'l', 'l', 'o', ' ', '4', '2'}
	return rt.MakeString(&b[0], 8, 0, true)
}

//pu:noinline
func f() { keepS = g() }

func main() {
	f()
	println(clobber(1000))
	println("string (intact if as written):", keepS)
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

