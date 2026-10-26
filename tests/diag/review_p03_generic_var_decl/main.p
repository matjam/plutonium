package main

// G[int] instantiated only in a var declaration's type.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }



//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	var g G[int]
	call(&g)
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
