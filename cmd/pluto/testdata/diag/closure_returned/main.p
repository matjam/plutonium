package main

// Each function returns a closure record made in its own frame, which is
// reclaimed when it returns (spec sections 4.5 and 6.3).

type Seq func(yield func(int) bool)

// A check before the literal makes this an ordinary function, not a
// closure constructor.
func CountChecked(n int) Seq {
	if n < 1 {
		panic("CountChecked: n must be positive")
	}
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

// Through a local variable.
func Var(n int) Seq {
	f := func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
	return f
}

type holder struct {
	f func() int
}

// In a struct field.
func Field(n int) holder {
	var h holder
	h.f = func() int { return n }
	return h
}

// In an interface.
func Any(n int) any {
	total := n * 2
	return func() int { return total }
}

type counter struct{ n int }

func (c *counter) get() int { return c.n }

// A method value.
func Method(c *counter) func() int {
	return c.get
}

func pass(f func() int) func() int { return f }

// Through a call that returns its argument.
func Through(n int) func() int {
	return pass(func() int { return n })
}

func Const(n int) func() int {
	return func() int { return n }
}

// The closure a closure constructor builds in this frame.
func Built(n int) func() int {
	m := n + 1
	return Const(m)
}

// A named result.
func Named(n int) (f func() int) {
	f = func() int { return n }
	return
}

// Through memory on the heap.
func Ptr(n int) *holder {
	h := new(holder)
	h.f = func() int { return n }
	return h
}

// Through a slice of a local array.
func Alias(n int) [1]func() int {
	var arr [1]func() int
	s := arr[:]
	s[0] = func() int { return n }
	return arr
}

func put(h *holder, f func() int) { h.f = f }

// Stored by a function it calls.
func Stored(n int) holder {
	var h holder
	put(&h, func() int { return n })
	return h
}

// Stored by another literal.
func Setter(n int) func() int {
	var g func() int
	set := func(f func() int) { g = f }
	set(func() int { return n })
	return g
}

func main() {
	Alias(1)
	Stored(1)
	Setter(1)
	CountChecked(1)
	Var(1)
	Field(1)
	Any(1)
	Method(&counter{})
	Through(1)
	Built(1)
	Named(1)
	Ptr(1)
}
