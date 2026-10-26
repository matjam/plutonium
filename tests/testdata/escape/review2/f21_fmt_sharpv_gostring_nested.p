package main

// %#v calls GoString of a nested value that keeps a pointer field
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type G struct {
	p *Inner
	n int
}

func (g G) GoString() string {
	keep = g.p
	return "G"
}

type Outer struct {
	A int
	B G
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Printf("%#v\n", Outer{1, G{&in, 2}})
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
