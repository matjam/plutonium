package main

// a callee keeps a union's pointer payload bound by a type switch
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

//pu:noinline
func store(u U) {
	switch v := u.(type) {
	case None:
	case P:
		keep = v
	}
}

//pu:noinline
func f() {
	var in Inner
	store(U.P(&in))
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
