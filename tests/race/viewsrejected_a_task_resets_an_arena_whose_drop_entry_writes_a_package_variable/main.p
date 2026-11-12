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
	n := a.New[Node]()
	n.n = 1
	var s sync.Scope
	s.Task(func() { a.Reset() })
	println(drops)
	s.Wait()
}
