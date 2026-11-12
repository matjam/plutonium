package main

import "pluto/sync"

var shared int

type Node struct{ p *int }

func touch(n *Node) { *n.p = 2 }

func main() {
	n := &Node{p: &shared}
	var s sync.Scope
	s.Task(func() { touch(n) })
	println(shared)
	s.Wait()
}
