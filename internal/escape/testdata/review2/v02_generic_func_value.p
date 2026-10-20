package main

// an instantiated generic function used as a func value
import "unsafe"

type Inner struct{ n int }

var keep *Inner

var keepAny any

func keepIt[T any](x T) { keepAny = x }

//pu:noinline
func f() {
	var in Inner
	k := keepIt[*Inner]
	k(&in)
	keep, _ = keepAny.(*Inner)
}

func main() {
	var mark int
	f()
	if keep == nil {
		println("not kept")
		return
	}
	// keep points below main's frame: into f's frame, which is gone.
	println("dangling:", uintptr(unsafe.Pointer(keep)) < uintptr(unsafe.Pointer(&mark)))
}
