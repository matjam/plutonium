package main

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		p := &i
		s.Task(func() { println(*p) })
	}
}
