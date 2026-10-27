package main

import "pluto/sync"

func main() {
	var s sync.Scope
	{
		var a [4]int
		b := a[:]
		s.Task(func() { b[0] = 1 })
	}
	s.Wait()
}
