package main

import (
	"pluto/arena"
	"pluto/sync"
)

var drops int

type Node owner struct{ n int }

func (n *Node) Drop() { drops++ }

type resetter interface{ Reset() }

func main() {
	a := arena.NewFixed(1024)
	n := a.New[Node]()
	n.n = 1
	var r resetter = &a
	var s sync.Scope
	s.Task(func() { r.Reset() })
	println(drops)
	s.Wait()
}
