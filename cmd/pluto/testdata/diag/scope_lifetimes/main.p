package main

// What a task started by sync.Scope may keep (language spec section 4.11):
// it takes the variables whose block may end before the Scope waits, but a
// view such a variable holds still looks at the storage it came from; a
// Scope that has started tasks cannot move; and an owner that a task takes
// is no longer the starting function's.

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func lines(text []string) {
	var s sync.Scope
	for _, line := range text {
		var buf [64]byte
		v := buf[:copy(buf[:], line)]
		s.Task(func() { send(v) })
	}
}

func wait(s sync.Scope) {}

func moved() {
	var s sync.Scope
	s.Task(func() { println("task") })
	wait(s)
}

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func owner() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		b := Buf{n: i}
		s.Task(func() { println(b.n) })
		println(b.n)
	}
}

func main() {
	lines(nil)
	moved()
	owner()
}
