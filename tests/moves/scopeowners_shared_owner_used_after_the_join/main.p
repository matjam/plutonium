package main

import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func take(b Buf) {}

func main() {
	b := Buf{}
	var s sync.Scope
	s.Task(func() { println(b.n) })
	s.Wait()
	take(b)
}
