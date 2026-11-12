package main

// unsafe.Drop of a value rather than a pointer to it (E3209).

import "unsafe"

func main() {
	x := 1
	unsafe.Drop(x)
}
