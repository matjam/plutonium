package main

import "pluto/sync"

var g = 42

func main() {
	var s sync.Scope
	{
		p := &g
		s.Task(func() { println(*p) })
	}
	s.Wait()
}
