package main

// G[int] only inside a map type; frame pointer to a copy of an element.
import "unsafe"

type Keeper interface{ Keep() }

var kept unsafe.Pointer

type G[T any] struct{ v T }

func (g *G[T]) Keep() { kept = unsafe.Pointer(g) }



//pu:noinline
func call(k Keeper) { k.Keep() }

//pu:noinline
func f() {
	m := map[int]G[int]{}
	m[1] = G[int]{}
	g := m[1]
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
