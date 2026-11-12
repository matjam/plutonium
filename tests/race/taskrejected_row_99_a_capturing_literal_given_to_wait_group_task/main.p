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
	var wg sync.WaitGroup
	for i := range n {
		wg.Task(func() { results[i] = f(i) })
	}
	wg.Wait()
	println(results[3])
}
