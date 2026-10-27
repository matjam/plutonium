package main

import "pluto/sync"

func main() {
	var s sync.Scope
	{
		y := 42
		s.Task(func() { println(y) })
	}
	s.Wait()
}
