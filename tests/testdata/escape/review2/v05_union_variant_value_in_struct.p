package main

// a union constructor stored in a struct field, then called
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

type H struct{ mk func(*Inner) U }

//pu:noinline
func g() U {
	var in Inner
	h := H{U.P}
	return h.mk(&in)
}

//pu:noinline
func f() {
	if v, ok := g().(U.P); ok {
		keep = v
	}
}

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
