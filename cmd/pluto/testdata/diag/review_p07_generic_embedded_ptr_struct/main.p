package main

// G[int] only as an embedded field of a struct type literal in a var.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }



//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	var h struct {
		G[int]
		pad int
	}
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
