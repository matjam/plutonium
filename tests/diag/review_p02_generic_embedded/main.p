package main

// G[int] instantiated only as an embedded field of a non-generic struct H; promoted pointer method.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }

type H struct {
	G[int]
	pad int
}

//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	var h H
	call(&h)
}

func main() {
	var mark int
	f()
	if kept == nil {
		println("method not called")
		return
	}
	println("dangling:", uintptr(kept) < uintptr(unsafe.Pointer(&mark)))
}
