package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Node struct{ v int }

var (
	mu    sync.Mutex
	nodes vec.Vec[owned[Node]]
	m     = map[int]*Node{}
)

func reader(done chan bool) {
	mu.Lock()
	p := m[1]
	mu.Unlock()
	println(p.v) // the remover may have freed the Node
	done <- true
}

func remover(done chan bool) {
	mu.Lock()
	delete(m, 1)
	nodes.Remove(0)
	mu.Unlock()
	done <- true
}

func main() {
	nodes.Push(new(Node{v: 1}))
	m[1] = &**nodes.Ptr(0)
	done := make(chan bool)
	task reader(done)
	task remover(done)
	<-done
	<-done
}
