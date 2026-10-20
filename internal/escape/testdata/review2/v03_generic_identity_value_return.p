package main

// an instantiated generic identity function value returns a frame pointer out of g
import "unsafe"

type Inner struct{ n int }

var keep *Inner

func id[T any](x T) T { return x }

//pu:noinline
func g() *Inner {
	var in Inner
	h := id[*Inner]
	return h(&in)
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
