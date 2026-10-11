package main

import "pluto/alloc"

type Node struct {
	name  string
	count shared[int]
}

var node = Node{name: "a"}

func consume(c chan any) {
	v := <-c
	println(v != nil)
}

// What an interface holds is known only from the conversions the whole
// program makes: a *Node in an any reaches a shared[int].
func main() {
	node.count = alloc.Shared[int]()
	c := make(chan any, 1)
	task consume(c)
	c <- &node
}
