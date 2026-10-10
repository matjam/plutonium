package main

import "pluto/arena"

// make of a slice is not permitted: nothing would own the array (language
// spec section 3). Each form gets its vec form; a constant size an array.

func fill[S ~[]E, E any](n int) S {
	return make(S, n) // a type parameter's slice type
}

func main() {
	n := 8
	buf := make([]byte, n)       // vec.Make
	list := make([]string, 0, n) // vec.WithCapacity
	both := make([]int, 2, n)    // vec.Make and Reserve
	var mem [256]byte
	a := arena.New(mem[:])
	in := make([]int, n, &a)    // with an allocator
	table := make([]uint16, 64) // a constant size: an array
	_ = make(chan int, 4)       // channels are still made
	println(len(buf), len(list), len(both), len(in), len(table), len(fill[[]int](2)))
}
