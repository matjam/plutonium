package main
import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func main() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	b.Drop()
}