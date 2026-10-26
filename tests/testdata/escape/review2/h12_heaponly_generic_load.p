package main

// g stores in the instance field; a generic function loads it
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	p *T
	n int
}

//pu:noinline
func get[T any](b *Box[T]) *T { return b.p }

//pu:noinline
func g() *Inner {
	var in Inner
	b := Box[Inner]{p: &in}
	return get(&b)
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
