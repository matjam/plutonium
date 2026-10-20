package main

// an alias of an unnamed struct type
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type A = struct {
	p *Inner
	n int
}

//pu:noinline
func mk(p *Inner) A { return A{p: p} }

//pu:noinline
func g() *Inner {
	var in Inner
	a := mk(&in)
	return a.p
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
