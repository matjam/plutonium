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
	b := arena.NewFixed(1024)
	n := a.New[Node]()
	n.n = 1
	p := b.New[int]()
	*p = 2
	var s sync.Scope
	s.Task(func() { b.Reset() })
	println(drops)
	s.Wait()
	a.Reset()
}
