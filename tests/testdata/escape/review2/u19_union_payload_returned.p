package main

// a callee returns the payload of a union holding a frame pointer; f's caller gets it
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

//pu:noinline
func payload(u U) *Inner {
	switch v := u.(type) {
	case None:
		return nil
	case P:
		return v
	}
}

//pu:noinline
func g() *Inner {
	var in Inner
	return payload(U.P(&in))
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
