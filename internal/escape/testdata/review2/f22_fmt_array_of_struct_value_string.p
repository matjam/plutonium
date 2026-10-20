package main

// %s of [2]E where E's value String keeps e.p
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
	arr := [2]E{{&in, 1}, {nil, 2}}
	fmt.Printf("%s\n", arr)
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
