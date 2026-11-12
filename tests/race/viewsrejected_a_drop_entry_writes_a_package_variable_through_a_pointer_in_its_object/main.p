package main

import (
	"pluto/arena"
	"pluto/sync"
)

var shared int

type Node owner struct{ p *int }

func (n *Node) Drop() { *n.p = 2 }

func main() {
	a := arena.NewFixed(1024)
	x := a.New[Node]()
	x.p = &shared
	var s sync.Scope
	s.Task(func() { a.Reset() })
	println(shared)
	s.Wait()
}
