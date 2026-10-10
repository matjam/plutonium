// A freestanding program (no operating system support in the runtime) that
// runs as a Linux process: start_amd64.s stands in for a kernel's entry
// point, console and halt.
package main

import (
	"pluto/rt"
	"unsafe"
)

func consoleWrite(p unsafe.Pointer, n int)

//pu:noreturn
func halt()

func init() {
	rt.SetConsole(consoleWrite)
	rt.SetPanicHandler(halt)
}

func get(xs []int, i int) int {
	return xs[i]
}

func main() {
	xs := []int{1, 2, 3}
	println(get(xs, 1))
	println(get(xs, 7))
}
