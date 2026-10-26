package main

// U.P as a func value, its result returned out of g (E3001 expected)
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

//pu:noinline
func g() U {
	var in Inner
	mk := U.P
	return mk(&in)
}

//pu:noinline
func f() {
	u := g()
	if v, ok := u.(U.P); ok {
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
