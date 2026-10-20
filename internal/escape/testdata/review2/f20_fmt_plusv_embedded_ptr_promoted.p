package main

// %+v of a struct embedding *Mid (promoted pointer String keeps its receiver) next to an int
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type Mid struct{ in *Inner }

func (m *Mid) String() string {
	keep = m.in
	return "mid"
}

type Outer struct {
	*Mid
	k int
}

type Top struct {
	O Outer
	z int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	m := Mid{&in}
	fmt.Printf("%+v\n", Top{Outer{&m, 1}, 2})
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
