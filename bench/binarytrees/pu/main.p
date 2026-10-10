// binarytrees: Benchmarks Game binary-trees. Allocates and walks many short-
// lived perfect binary trees plus one long-lived tree.
// Measures allocation/deallocation throughput and pointer chasing.
//
// Trees are freed explicitly, like the C version: Plutonium's raw pointers
// are never freed by the compiler (owned[T] would free them automatically).
package main

import "pluto/fmt"
import "pluto/alloc"

const MaxDepth = 18

type Node struct {
	left  *Node
	right *Node
}

func bottomUpTree(depth int64) *Node {
	n := alloc.New[Node]()
	if depth > 0 {
		n.left = bottomUpTree(depth - 1)
		n.right = bottomUpTree(depth - 1)
	}
	return n
}

func itemCheck(n *Node) int64 {
	if n.left == nil {
		return 1
	}
	return 1 + itemCheck(n.left) + itemCheck(n.right)
}

func freeTree(n *Node) {
	if n.left != nil {
		freeTree(n.left)
		freeTree(n.right)
	}
	alloc.Free(n)
}

func stretch(depth int64) int64 {
	t := bottomUpTree(depth)
	c := itemCheck(t)
	freeTree(t)
	return c
}

func main() int64 {
	minDepth := int64(4)
	maxDepth := int64(MaxDepth)
	if minDepth+2 > maxDepth {
		maxDepth = minDepth + 2
	}

	fmt.Printf("%d %d\n", maxDepth+1, stretch(maxDepth+1))

	longLived := bottomUpTree(maxDepth)

	for d := minDepth; d <= maxDepth; d += 2 {
		iterations := int64(1) << (maxDepth - d + minDepth)
		check := int64(0)
		for i := int64(0); i < iterations; i++ {
			t := bottomUpTree(d)
			check += itemCheck(t)
			freeTree(t)
		}
		fmt.Printf("%d %d %d\n", iterations, d, check)
	}

	fmt.Printf("%d %d\n", maxDepth, itemCheck(longLived))
	freeTree(longLived)
	return 0
}
