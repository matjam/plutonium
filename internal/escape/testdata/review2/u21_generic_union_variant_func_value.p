package main

// Opt[*Inner].Some used as a func value
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Opt[T any] union {
	None
	Some T
}

//pu:noinline
func store(o Opt[*Inner]) {
	if p, ok := o.(Opt[*Inner].Some); ok {
		keep = p
	}
}

//pu:noinline
func f() {
	var in Inner
	mk := Opt[*Inner].Some
	store(mk(&in))
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
