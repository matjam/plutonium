package main

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func main() {
	for _, line := range []string{"a"} {
		var s sync.Scope
		var buf [8]byte
		v := buf[:copy(buf[:], line)]
		s.Task(func() { send(v) })
	}
}
