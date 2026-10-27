package main

import "pluto/sync"

func main() {
	var s sync.Scope
	var p *int
	{
		y := 42
		p = &y
	}
	s.Task(func() { println(*p) })
	s.Wait()
}
