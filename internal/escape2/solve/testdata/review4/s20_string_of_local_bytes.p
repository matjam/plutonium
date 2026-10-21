package main

import (
)

//pu:noinline
func f() {
	b := [8]byte{'h', 'e', 'l', 'l', 'o', ' ', '4', '2'}
	keepS = string(b[:])
}

var keepS string

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

