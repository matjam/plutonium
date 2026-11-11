package main

import "pluto/sync"

// A closure stored in the same frame, or written as the argument of s.Task
// on a local Scope, may capture the function's variables (language spec
// section 11.5, D68).

func sameFrame() int {
	x := 1
	f := func() { x++ }
	var fs [2]func()
	fs[0] = f
	var g func() = fs[0]
	g()
	fs[0]()
	return x
}

func scopedTask() int {
	x := 1
	var s sync.Scope
	s.Task(func() { x += 10 })
	s.Wait()
	return x
}

func main() {
	println(sameFrame())
	println(scopedTask())
}
