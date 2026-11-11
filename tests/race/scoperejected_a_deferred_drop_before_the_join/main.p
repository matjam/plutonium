package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

// A panic between Task and Wait runs the deferred Drop while the task runs.
func panicPath() {
	w := vec.New[int]()
	defer w.Drop()
	var s sync.Scope
	s.Task(func() { w.Push(1) })
	s.Wait()
}

// A return before the Wait runs the deferred Drop before the Scope joins.
func returnPath(early bool) {
	var b Buf
	defer b.Drop()
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	if early {
		return
	}
	s.Wait()
}

func main() {
	panicPath()
	returnPath(false)
}
