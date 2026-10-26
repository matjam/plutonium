package main

// The generic type is unexported in package gen and instantiated only in
// gen's declaration of H; main converts *H to an interface and the promoted
// pointer method keeps its receiver.
import (
	"unsafe"

	"example.com/review/gen"
)

//pu:noinline
func call(k gen.Keeper) { k.Keep() }

//pu:noinline
func f() {
	var h gen.H
	call(&h)
}

func main() {
	var mark int
	f()
	if gen.Kept == nil {
		println("method not called")
		return
	}
	println("dangling:", uintptr(gen.Kept) < uintptr(unsafe.Pointer(&mark)))
}
