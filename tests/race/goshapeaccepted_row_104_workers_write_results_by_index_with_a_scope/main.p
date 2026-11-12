package main

import (
	"pluto/sync"
	"pluto/vec"
)

func worker(r *int, i int) {
	*r = i * i
}

func main() {
	v := vec.Make[int](4)
	results := v.Slice()
	var s sync.Scope
	for i := range 4 {
		s.Task(func() { worker(&results[i], i) })
	}
	s.Wait()
	println(results[3])
}
