package main

// a struct with a Stringer field holding &in
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

func (i *Inner) String() string {
	keep = i
	return "inner"
}

type Outer struct {
	s fmt.Stringer
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(Outer{&in, 1})
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
