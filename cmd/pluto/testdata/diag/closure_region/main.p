package main

// Closure regions (spec section 4.5): a call in a loop reuses its region on
// every iteration, and a region cannot hold itself.

func counterFrom(n int) func() int {
	return func() int {
		n++
		return n
	}
}

// Keeps the closure of a call in the loop past the iteration.
func KeepLast(sizes []int) int {
	var last func() int
	for _, n := range sizes {
		last = counterFrom(n)
	}
	return last()
}

type box struct {
	f func() int
}

// The same, through a pointer.
func KeepIn(b *box, sizes []int) {
	for _, n := range sizes {
		b.f = counterFrom(n)
	}
}

// Fine: the closure is used within the iteration.
func UseEach(sizes []int) int {
	total := 0
	for _, n := range sizes {
		next := counterFrom(n)
		total += next()
	}
	return total
}

// Returns closures wrapping the closures of a call of itself: the region
// would have to hold itself.
func chain(n int) func() int {
	if n == 0 {
		return func() int { return 0 }
	}
	next := chain(n - 1)
	return func() int { return n + next() }
}

// Mutual recursion, through a func value.
var pick func(int) func() int

func even(n int) func() int {
	inner := pick(n - 1)
	return func() int { return inner() + 1 }
}

func main() {
	KeepLast([]int{1, 2})
	KeepIn(&box{}, []int{1})
	println(UseEach([]int{1, 2}))
	chain(3)
	pick = even
	even(2)
}
