package main

// a union holding a generic union holding a pointer
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Opt[T any] union {
	None
	Some T
}

type U union {
	Empty
	O Opt[*Inner]
}

//pu:noinline
func store(u U) {
	switch v := u.(type) {
	case Empty:
	case O:
		if p, ok := v.(Opt[*Inner].Some); ok {
			keep = p
		}
	}
}

//pu:noinline
func f() {
	var in Inner
	store(U.O(Opt[*Inner].Some(&in)))
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
