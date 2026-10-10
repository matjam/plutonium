// heapsort: in-place heapsort of N pseudo-random int64 values from a 64-bit
// LCG. Measures data-dependent branches, random memory access, and uint64
// wrap-around multiply. Prints a sortedness flag and two hashes of the result.
package main

import "pluto/fmt"

const N = 5000000

var data [N]int64

var seed uint64 = 42

func nextRand() int64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int64(seed >> 33)
}

func siftDown(start int64, end int64) {
	root := start
	for {
		child := 2*root + 1
		if child > end {
			return
		}
		swap := root
		if data[swap] < data[child] {
			swap = child
		}
		if child+1 <= end && data[swap] < data[child+1] {
			swap = child + 1
		}
		if swap == root {
			return
		}
		t := data[root]
		data[root] = data[swap]
		data[swap] = t
		root = swap
	}
}

func heapsort(n int64) {
	for start := (n - 2) / 2; start >= 0; start-- {
		siftDown(start, n-1)
	}
	for end := n - 1; end > 0; end-- {
		t := data[0]
		data[0] = data[end]
		data[end] = t
		siftDown(0, end-1)
	}
}

func main() int64 {
	for i := int64(0); i < N; i++ {
		data[i] = nextRand()
	}
	heapsort(N)
	sorted := int64(1)
	hash := uint64(0)
	for i := int64(0); i < N; i++ {
		if i > 0 && data[i-1] > data[i] {
			sorted = 0
		}
		hash = hash*31 + uint64(data[i])
	}
	fmt.Printf("%d\n", sorted)
	fmt.Printf("%d\n", data[N/2])
	fmt.Printf("%d\n", int64(hash%1000000007))
	return 0
}
