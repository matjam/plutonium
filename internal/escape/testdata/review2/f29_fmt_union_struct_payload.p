package main

// a union whose payload is a two-word struct whose value String keeps a pointer
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type W struct {
	p *Inner
	n int
}

func (w W) String() string {
	keep = w.p
	return "w"
}

type U union {
	None
	S W
}

type Outer struct {
	u U
	k int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Println(Outer{U.S(W{&in, 1}), 2})
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
