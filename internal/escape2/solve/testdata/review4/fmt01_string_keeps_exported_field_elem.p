package main

// a named slice type with a String that keeps an element pointer
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type L []*Inner

func (l L) String() string {
	keep = l[0]
	return "l"
}

type Outer struct {
	L L
	K int
}

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	arr := [1]*Inner{&in}
	fmt.Println(Outer{L(arr[:]), 1})
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
