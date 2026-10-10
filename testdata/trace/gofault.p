package main

// A hardware fault in a puroutine, on a worker thread with its own signal
// stack, is reported with the puroutine and its stack.

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
	n := new(node)
	n.next = new(node)
	go worker(n, done)
	println(<-done)
	go worker(n.next, done)
	println(<-done)
}
