package main

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func main() {
	var s sync.Scope
	for _, line := range []string{"a"} {
		var buf [8]byte
		n := copy(buf[:], line)
		s.Task(func() { send(buf[:n]) })
	}
}
