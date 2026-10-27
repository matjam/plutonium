package main

import "pluto/sync"

//pu:noinline
func f(c bool) {
	var s sync.Scope
	if c {
		y := 1
		p := &y
		s.Task(func() { println(*p) })
	}
}

func main() { f(true) }
