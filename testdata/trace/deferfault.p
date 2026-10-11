package main

// A hardware fault runs deferred calls as a panic does: here a nil pointer
// dereference, which the hardware reports with SIGSEGV, in the body of a
// range-over-func loop. The runtime leaves the signal handler and runs the
// deferred calls of the iterator, of the function the loop is in, and of
// main on the faulting task's own stack.

type node struct {
	next *node
	val  int
}

// Nodes is a closure constructor: the iterator yields head and the nodes
// after it, then a nil one.
func Nodes(head *node) func(yield func(*node) bool) {
	return func(yield func(*node) bool) {
		defer println("iterator deferred")
		n := head
		for {
			if !yield(n) {
				return
			}
			n = n.next
		}
	}
}

func sum(head *node) (total int) {
	defer func() {
		println("sum deferred, total =", total)
	}()
	for n := range Nodes(head) {
		total += n.val
	}
	return total
}

func main() {
	defer println("main deferred")
	list := &node{val: 1, next: &node{val: 2}}
	println(sum(list))
}
