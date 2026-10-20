package main

// Control: G[int] instantiated by a composite literal; pointer method keeps receiver; called through an interface.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }



//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	g := G[int]{v: 1}
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
