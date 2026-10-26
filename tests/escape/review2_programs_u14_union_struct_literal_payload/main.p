package main

// U.S{...} composite payload holding a pointer
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	S struct {
		p *Inner
		n int
	}
}

//pu:noinline
func store(u U) {
	switch v := u.(type) {
	case None:
	case S:
		keep = v.p
	}
}

//pu:noinline
func f() {
	var in Inner
	store(U.S{p: &in, n: 1})
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
