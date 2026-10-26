package main

import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func take(b Buf) {}

func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		b := Buf{n: i}
		s.Task(func() { println(b.n) })
		println(b.n)
	}
}
