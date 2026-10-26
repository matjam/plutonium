package main

// an any field holding a Stringer holding &in (iface in iface)
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
	a any
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	var s fmt.Stringer = &in
	fmt.Println(Outer{s, 1})
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
