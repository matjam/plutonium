package main

import "pluto/sync"

func main() {
	var s sync.Scope
	{
		y := 42
		p := &y
		println(*p)
		s.Task(func() { println(1) })
	}
	s.Wait()
}
