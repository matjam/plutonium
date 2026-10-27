package main

import "pluto/sync"

//pu:noinline
func f(c bool) {
	var s sync.Scope
	{
		y := 42
		p := &y
		s.Task(func() { println(*p) })
		if c {
			return
		}
		s.Wait()
	}
}

func main() { f(false) }
