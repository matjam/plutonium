package main

import (
	"pluto/fmt"
)

//pu:noinline
func g() string {
	var eb fmt.ErrorBuf
	return eb.Errorf("hello %d", 42).Error()
}

//pu:noinline
func f() { keepS = g() }

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

