package main

// G[int] only through new and copied into a frame variable.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }



//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	p := new(G[int])
	g := *p
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
