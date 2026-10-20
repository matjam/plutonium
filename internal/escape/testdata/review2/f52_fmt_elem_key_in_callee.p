package main

// the k-series shape bug through fmt: a callee prints &p[1] where *S has a String that keeps it
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

//pu:noinline
func g(p *[2]Inner) { fmt.Println(&p[1]) }

//pu:noinline
func f() {
	var arr [2]Inner
	g(&arr)
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
