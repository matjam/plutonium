package main

// a union method keeps its payload, called through an interface
import "unsafe"

type Inner struct{ n int }

var keep *Inner

type U union {
	None
	P *Inner
}

func (u U) Keep() {
	switch v := u.(type) {
	case None:
	case P:
		keep = v
	}
}

type Keeper interface{ Keep() }

//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	var in Inner
	call(U.P(&in))
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
