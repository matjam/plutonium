package main

import (
	"pluto/fmt"
)

func fail(eb *fmt.ErrorBuf) (int, error) { return 0, eb.Errorf("hello %d", 42) }

//pu:noinline
func f() {
	var eb fmt.ErrorBuf
	n := fail(&eb) or {
		keepErr = err
		return
	}
	_ = n
}

var keepErr error

func main() {
	f()
	println(clobber(1000))
	if keepErr == nil {
		println("nothing kept")
		return
	}
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

