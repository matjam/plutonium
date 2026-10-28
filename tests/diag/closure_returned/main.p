package main

// Each function returns a closure record that cannot be built in the region
// its caller provides (spec section 11.5), so it would outlive its frame.

type Seq func(yield func(int) bool)

type holder struct {
	f func() int
}

// Through memory on the heap: the result holds the closure only through a
// pointer, so the caller provides no region.
func Ptr(n int) owned[holder] {
	h := new(holder)
	h.f = func() int { return n }
	return h
}

func counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

// The same, for the closure of a call.
func PtrCall() owned[holder] {
	h := new(holder)
	h.f = counter()
	return h
}

type box struct{ n int }

func (b *box) get() int { return b.n }

// A method value, through a pointer.
func PtrMethod(b *box) owned[holder] {
	h := new(holder)
	h.f = b.get
	return h
}

// Made by a function literal nested in the function.
func Nested(n int) func() int {
	var g func() int
	build := func() { g = func() int { return n } }
	build()
	return g
}

// Made in the body of a range-over-func loop, a function of its own.
func FromLoop(seq Seq) func() int {
	for x := range seq {
		return func() int { return x }
	}
	return nil
}

func one(yield func(int) bool) { yield(1) }

func main() {
	Ptr(1)
	PtrCall()
	PtrMethod(&box{})
	Nested(1)
	FromLoop(one)
}
