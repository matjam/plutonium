package main

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func main() {
	var buf [8]byte
	n := 3
	var s sync.Scope
	s.Task(func() { send(buf[:n]) })
	p := &n
	s.Task(func() { println(*p) })
}
