package main

import "pluto/sync"

func main() {
	var s sync.Scope
	s.Task(func() {
		var t sync.Scope
		{
			y := 42
			p := &y
			t.Task(func() { println(*p) })
		}
		t.Wait()
	})
	s.Wait()
}
