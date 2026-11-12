package main

import (
	"pluto/sync"
	"pluto/vec"
)

func f(i int) int { return i * i }

func main() {
	n := 4
	v := vec.Make[int](n)
	results := v.Slice()
	var s sync.Scope
	for i := range n {
		s.Task(func() { results[i] = f(i) })
	}
	s.Wait()
	println(results[3])
}
