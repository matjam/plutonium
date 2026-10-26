package main

// a generic union instantiated with *Inner (pointer String keeps receiver)
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

type U[T any] union {
	None
	P T
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(U[*Inner].P(&in))
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
