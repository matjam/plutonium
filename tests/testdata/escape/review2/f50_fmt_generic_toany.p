package main

// a pointer-shaped W converted to any only inside a generic function, nested in a struct
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type W struct{ p *Inner }

func (w W) String() string {
	keep = w.p
	return "w"
}

//pu:noinline
func toAny[T any](x T) any { return x }

type Outer struct {
	a any
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(Outer{toAny(W{&in}), 1})
}

func main() {
	var mark int
	f()
	if keep == nil {
		println("method not called")
		return
	}
	// keep points below main's frame: into f's frame, which is gone.
	println("dangling:", uintptr(unsafe.Pointer(keep)) < uintptr(unsafe.Pointer(&mark)))
}
