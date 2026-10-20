package main

// a generic constructor stores the pointer in Box[T].p; non-generic g loads Box[Inner].p and returns it
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	p *T
	n int
}

//pu:noinline
func mk[T any](p *T) Box[T] { return Box[T]{p: p, n: 1} }

//pu:noinline
func g() *Inner {
	var in Inner
	b := mk(&in)
	return b.p
}

//pu:noinline
func f() { keep = g() }

func main() {
	var mark int
	f()
	if keep == nil {
		println("not kept")
		return
	}
	// keep points below main's frame: into f's frame, which is gone.
	println("dangling:", uintptr(unsafe.Pointer(keep)) < uintptr(unsafe.Pointer(&mark)))
}
