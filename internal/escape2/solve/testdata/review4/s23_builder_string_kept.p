package main

import (
	"pluto/strings"
)

//pu:noinline
func f() {
	var sb strings.Builder
	sb.WriteString("hello 42")
	keepS = sb.String()
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

