package main

import (
	"pluto/fmt"
)

type S struct{ r *error; e error }

func link(s *S) { *s.r = s.e }

var keepErr error

//pu:noinline
func f() {
	var eb fmt.ErrorBuf
	var out error
	s := S{&out, eb.Errorf("hello %d", 42)}
	link(&s)
	keepErr = out
}

func main() {
	f()
	println(clobber(1000))
	println("error (intact if as written):", keepErr.Error())
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

