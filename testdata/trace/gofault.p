package main

// A hardware fault in a task, on a worker thread with its own signal
// stack, is reported with the task and its stack.

type node struct {
	next *node
	v    int
}

func second(n *node) int {
	return n.next.v
}

func worker(n *node, done chan int) {
	done <- second(n)
}

func main() {
	done := make(chan int)
	n := &nodes[0]
	n.next = &nodes[1]
	task worker(n, done)
	println(<-done)
	task worker(n.next, done)
	println(<-done)
}

// Package variables: the tasks may outlive main's frame.
var nodes [2]node
