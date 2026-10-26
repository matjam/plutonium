package main

// as f25 with a pointer-shaped error type (one pointer field) and %v
import (
	"pluto/fmt"
	"unsafe"
)

type Inner struct{ n int }

var keep *Inner

type E struct{ p *Inner }

func (e E) Error() string {
	keep = e.p
	return "e"
}

//pu:noinline
func get(p *Inner) (int, error) { return 0, E{p} }

//pu:noinline
func f() {
	var in Inner
	in.n = 42
	x := get(&in) or {
		fmt.Printf("%v\n", err)
		return
	}
	println(x)
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
