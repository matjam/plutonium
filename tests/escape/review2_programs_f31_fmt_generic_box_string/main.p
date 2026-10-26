package main

// a generic Box[T] with a String that keeps its T in an any
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

var keepAny any

type Box[T any] struct {
	v T
	n int
}

func (b Box[T]) String() string {
	keepAny = b.v
	return "box"
}

type Outer struct {
	b Box[*Inner]
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(Outer{Box[*Inner]{&in, 1}, 2})
	keep, _ = keepAny.(*Inner)
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
