package main

import (
	"pluto/container/heap"
	"pluto/fmt"
)

// container/heap, checked against Go's: the same operations on the same
// data leave the underlying array in the same order, which depends on the
// exact sift-up and sift-down steps.

type IntHeap []int

func (h IntHeap) Len() int           { return len(h) }
func (h IntHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h IntHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *IntHeap) Push(x any) {
	n := len(*h)
	*h = (*h)[:n+1]
	(*h)[n] = x.(int)
}

func (h *IntHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

var seed uint64 = 99

func rnd(n int) int {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int(seed>>33) % n
}

func main() {
	h := IntHeap(make([]int, 0, 1000))
	for i := 0; i < 40; i++ {
		n := len(h)
		h = h[:n+1]
		h[n] = rnd(100)
	}
	fmt.Println("before Init", []int(h))
	heap.Init(&h)
	fmt.Println("after Init", []int(h))

	heap.Push(&h, 3)
	heap.Push(&h, 101)
	heap.Push(&h, -5)
	fmt.Println("after Push", []int(h))

	fmt.Println("Pop", heap.Pop(&h), heap.Pop(&h), heap.Pop(&h))
	fmt.Println("after Pop", []int(h))

	fmt.Println("Remove", heap.Remove(&h, 7), heap.Remove(&h, 0), heap.Remove(&h, len(h)-1))
	fmt.Println("after Remove", []int(h))

	for i := 0; i < 30; i++ {
		j := rnd(len(h))
		h[j] = rnd(200) - 50
		heap.Fix(&h, j)
	}
	fmt.Println("after Fix", []int(h))

	out := make([]int, 0, 64)
	for h.Len() > 0 {
		n := len(out)
		out = out[:n+1]
		out[n] = heap.Pop(&h).(int)
	}
	fmt.Println("drained", out)
}
