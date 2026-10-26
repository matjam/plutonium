package main

// an embedded generic Box, the field read through promotion
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type Box[T any] struct {
	p *T
	n int
}

type O struct {
	Box[Inner]
	k int
}

//pu:noinline
func (b *Box[T]) Set(p *T) { b.p = p }

//pu:noinline
func g() *Inner {
	var in Inner
	var o O
	o.Set(&in)
	return o.p
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
