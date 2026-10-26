package main

import "pluto/sync"

type Buf owner struct{ n int }

func (b *Buf) Drop() {}

func take(b Buf) {}

func main() {
	var s sync.Scope
	s.Task(func() {})
	t := s
	t.Wait()
}
