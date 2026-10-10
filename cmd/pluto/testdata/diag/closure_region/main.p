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

func filter(it func(func(int) bool), keep func(int) bool) func(func(int) bool) {
	return func(yield func(int) bool) {
		it(func(x int) bool { return !keep(x) || yield(x) })
	}
}

func count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n && yield(i); i++ {
		}
	}
}

// Composes closures in a loop: each would hold the one the same call made
// before, in the region the call refills.
func Compose(mods []int) func(func(int) bool) {
	it := count(100)
	for _, m := range mods {
		it = filter(it, func(x int) bool { return x%m != 0 })
	}
	return it
}

func wrap(g func() int, n int) func() int {
	return func() int { return g()*10 + n }
}

func one() int { return 1 }

// The variable of a for statement passes its value on to the next
// iteration.
func Nest(n int) int {
	i := 0
	for g := wrap(one, 0); ; g = wrap(g, i) {
		i++
		if i == n {
			return g()
		}
	}
}

// A goto back to a label before the call makes a loop.
func Again(n int) int {
	g := wrap(one, 0)
	i := 0
again:
	g = wrap(g, i)
	i++
	if i < n {
		goto again
	}
	return g()
}

func main() {
	KeepLast([]int{1, 2})
	KeepIn(&box{}, []int{1})
	println(UseEach([]int{1, 2}))
	Compose([]int{2, 3})
	println(Nest(3), Again(3))
	chain(3)
	pick = even
	even(2)
}
