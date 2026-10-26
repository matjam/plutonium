package main

// a generic union U[T] instantiated with *Inner; a generic callee keeps the payload in an any
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U[T any] union {
	None
	P T
}

var keepAny any

//pu:noinline
func store[T any](u U[T]) {
	switch v := u.(type) {
	case None:
	case P:
		keepAny = v
	}
}

//pu:noinline
func f() {
	var in Inner
	store(U[*Inner].P(&in))
	keep, _ = keepAny.(*Inner)
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
