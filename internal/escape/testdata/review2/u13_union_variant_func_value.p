package main

// U.P used as a func value
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
	mk := U.P
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
