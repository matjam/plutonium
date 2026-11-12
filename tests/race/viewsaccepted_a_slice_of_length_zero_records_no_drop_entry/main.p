package main

import (
	"pluto/arena"
	"pluto/sync"
)

var drops int

type Node owner struct{ n int }

func (n *Node) Drop() { drops++ }

func main() {
	a := arena.NewFixed(1024)
	xs := a.Slice[Node](0)
	println(len(xs))
	var s sync.Scope
	s.Task(func() { a.Reset() })
	println(drops)
	s.Wait()
}
