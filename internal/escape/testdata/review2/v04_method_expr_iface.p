package main

// an interface method expression used as a func value
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type K interface{ K() }

type S struct{ in *Inner }

func (s *S) K() { keep = s.in }

//pu:noinline
func f() {
	var in Inner
	s := S{&in}
	m := K.K
	m(&s)
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
