package main

// a method expression (*S).keepIt used as a func value
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type S struct{ in *Inner }

func (s *S) keepIt() { keep = s.in }

//pu:noinline
func f() {
	var in Inner
	s := S{&in}
	m := (*S).keepIt
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
