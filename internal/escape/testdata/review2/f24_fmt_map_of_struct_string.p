package main

// map[int]E with a frame pointer: expected rejected (map is heap)
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type E struct {
	p *Inner
	n int
}

func (e E) String() string {
	keep = e.p
	return "e"
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	m := map[int]E{1: {&in, 1}}
	fmt.Println(m)
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
