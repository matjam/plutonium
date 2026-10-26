package main
import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func consume(b Buf) {}

func main() {
	var b Buf
	var s sync.Scope
	s.Task(func() { b.n = 1 })
	consume(b)
}