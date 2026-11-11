package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

// A deferred Wait armed ahead of the first Task runs first.
func waitDeferred() {
	w := vec.New[int]()
	defer w.Drop()
	var s sync.Scope
	defer s.Wait()
	s.Task(func() { w.Push(1) })
}

// A Drop deferred after the Wait.
func afterWait() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	s.Wait()
	defer b.Drop()
}

// A Drop deferred with no task that uses its owner.
func otherOwner() {
	var b Buf
	defer b.Drop()
	n := 0
	var s sync.Scope
	s.Task(func() { n = 1 })
	s.Wait()
	println(n)
}

func main() {
	waitDeferred()
	afterWait()
	otherOwner()
	println("ok")
}
