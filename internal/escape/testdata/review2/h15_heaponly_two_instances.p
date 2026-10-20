package main

// Box[Inner] stores heap only in one function, Box[T] stores a parameter elsewhere
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	p *T
	n int
}

var heapIn = Inner{1}

//pu:noinline
func wrap[T any](p *T) *T {
	var b Box[T]
	b.p = p
	return b.p
}

//pu:noinline
func g() *Inner {
	var in Inner
	return wrap(&in)
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
