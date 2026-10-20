package main

// a generic type whose field is an unnamed struct
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	s struct {
		p *T
		n int
	}
}

//pu:noinline
func mk[T any](p *T) Box[T] {
	var b Box[T]
	b.s.p = p
	return b
}

//pu:noinline
func g() *Inner {
	var in Inner
	b := mk(&in)
	return b.s.p
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
