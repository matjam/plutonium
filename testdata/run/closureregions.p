// Functions that return closures. In Plutonium the closures, and the
// variables they capture, are built in a region the caller provides
// (language spec section 4.5); the results must be Go's.

package main

type Seq func(yield func(int) bool)

// Count checks its argument before returning the iterator.
func Count(n int) Seq {
	if n < 0 {
		panic("Count: negative")
	}
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

// Steps returns one of two iterators, with early returns.
func Steps(n int, down bool) Seq {
	if n == 0 {
		return func(yield func(int) bool) {}
	}
	if down {
		return func(yield func(int) bool) {
			for i := n; i > 0; i-- {
				if !yield(i) {
					return
				}
			}
		}
	}
	step := 2
	return func(yield func(int) bool) {
		for i := 0; i < n*step; i += step {
			if !yield(i) {
				return
			}
		}
	}
}

// Counter captures a local that each call changes.
func Counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

// Pair returns two closures sharing one variable.
func Pair() (func(int), func() int) {
	x := 0
	return func(v int) { x = v }, func() int { return x }
}

// Later captures a local changed after the closure is made: by reference,
// as in Go.
func Later() func() int {
	v := 1
	f := func() int { return v * 10 }
	v = 7
	return f
}

// Adder and Twice return closures through two and three levels.
func Adder(k int) func(int) int {
	return func(x int) int { return x + k }
}

func Compose(f, g func(int) int) func(int) int {
	return func(x int) int { return g(f(x)) }
}

func Twice(k int) func(int) int {
	a := Adder(k)
	return Compose(a, a)
}

func Thrice(k int) func(int) int {
	return Compose(Twice(k), Adder(k))
}

// Wrap returns the closure of a call unchanged.
func Wrap(n int) Seq {
	return Count(n)
}

// Handler holds closures in a struct result.
type Handler struct {
	name string
	run  func() string
	next func() int
}

func NewHandler(name string) Handler {
	calls := 0
	return Handler{
		name: name,
		run:  func() string { calls++; return name },
		next: func() int { return calls },
	}
}

// Box is a type with closure-returning methods.
type Box struct{ items []int }

func (b *Box) All() func(func(int, int) bool) {
	return func(yield func(int, int) bool) {
		for i, x := range b.items {
			if !yield(i, x) {
				return
			}
		}
	}
}

func (b Box) Sum() func() int {
	total := 0
	for _, x := range b.items {
		total += x
	}
	return func() int { return total }
}

// Summer is satisfied by Box: its method returns a closure.
type Summer interface{ Sum() func() int }

// Generic closure-returning functions and methods.
func Repeat[T any](x T, n int) func(func(T) bool) {
	return func(yield func(T) bool) {
		for i := 0; i < n; i++ {
			if !yield(x) {
				return
			}
		}
	}
}

type List[T any] struct{ xs []T }

func (l *List[T]) Each() func(func(T) bool) {
	return func(yield func(T) bool) {
		for _, x := range l.xs {
			if !yield(x) {
				return
			}
		}
	}
}

func (l *List[T]) Last() func() T {
	var zero T
	if len(l.xs) == 0 {
		return func() T { return zero }
	}
	last := l.xs[len(l.xs)-1]
	return func() T { return last }
}

// AsAny returns a closure in an interface.
func AsAny(s string) any {
	return func() string { return s }
}

// MethodValue returns a method value.
func MethodValue(b *Box) func() func(func(int, int) bool) {
	return b.All
}

// count returns how many values s yields, and the last.
func count(s Seq) (int, int) {
	n, last := 0, -1
	for x := range s {
		n++
		last = x
	}
	return n, last
}

func main() {
	println(count(Count(4)))
	for x := range Steps(3, true) {
		print(x, " ")
	}
	println()
	for x := range Steps(3, false) {
		print(x, " ")
	}
	println()
	println(count(Steps(0, false)))

	c := Counter()
	c()
	c()
	println(c())
	d := Counter()
	println(d(), c())

	set, get := Pair()
	set(42)
	println(get())
	println(Later()())

	println(Adder(1)(1), Twice(2)(1), Thrice(3)(1))
	println(count(Wrap(5)))

	h := NewHandler("h")
	println(h.name, h.run(), h.run(), h.next())

	b := &Box{items: []int{3, 4, 5}}
	for i, x := range b.All() {
		print(i, ":", x, " ")
	}
	println()
	var s Summer = *b
	println(s.Sum()())
	all := MethodValue(b)
	n := 0
	for range all() {
		n++
	}
	println(n)

	// As func values.
	mk := Counter
	m := mk()
	m()
	println(m())
	cnt := Count
	println(count(cnt(2)))
	sum := (*b).Sum
	println(sum()())

	for x := range Repeat("ab", 2) {
		print(x, " ")
	}
	println()
	l := &List[string]{xs: []string{"x", "y"}}
	for x := range l.Each() {
		print(x, " ")
	}
	println(l.Last()())
	var e List[int]
	println(e.Last()())

	println(AsAny("q").(func() string)())
}
