package main

// the Box is on the heap, its field set through a generic method to a frame pointer: E3002 expected
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	p *T
	n int
}

var global Box[Inner]

//pu:noinline
func (b *Box[T]) Set(p *T) { b.p = p }

//pu:noinline
func f() {
	var in Inner
	global.Set(&in)
	keep = global.p
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
