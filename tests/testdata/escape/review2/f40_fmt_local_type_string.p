package main

// a type declared inside f, nested in a struct
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	type Outer struct {
		p *Inner
		k int
	}
	type Top struct {
		o Outer
		w W
	}
	fmt.Println(Top{Outer{&in, 1}, W{&in, 2}})
}

type W struct {
	p *Inner
	n int
}

func (w W) String() string {
	keep = w.p
	return "w"
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
