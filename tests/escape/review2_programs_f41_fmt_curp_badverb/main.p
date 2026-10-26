package main

// p.curP: a bad verb on a value whose earlier field's String ran
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

type Outer struct {
	w W
	b bool
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	fmt.Printf("%x\n", Outer{W{&in, 1}, true})
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
