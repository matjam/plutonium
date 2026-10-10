// binarytrees: Benchmarks Game binary-trees. Allocates and walks many short-
// lived perfect binary trees plus one long-lived tree.
// Measures allocation/deallocation throughput and pointer chasing.
//
// No explicit frees: per docs/plans/2026-03-15-memory-lifecycle-design.md,
// each tree is owned by the local variable it is assigned to (t, longLived)
// and freed when that variable's scope ends; child pointers stored in struct
// fields are owned by their parent node and released recursively with it.
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

func stretch(depth int64) int64 {
	t := bottomUpTree(depth)
	return itemCheck(t)
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
		}
		fmt.Printf("%d %d %d\n", iterations, d, check)
	}

	fmt.Printf("%d %d\n", maxDepth, itemCheck(longLived))
	return 0
}
