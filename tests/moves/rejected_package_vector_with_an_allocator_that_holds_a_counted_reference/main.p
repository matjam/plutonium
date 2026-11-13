package main

import (
	"pluto/alloc"
	"pluto/vec"
	"unsafe"
)

// An allocator of the program holds a counted reference of the type of a
// and replaces it when it allocates. A vector in a package variable may
// allocate through it: growing the vector may free the object the view
// of a looks at, so the view ends at the call (language spec section
// 18.3, 18.9).

type keeper struct {
	ref shared[vec.Vec[int]]
}

func (k *keeper) Alloc(size, align uintptr) unsafe.Pointer {
	k.ref = alloc.Shared[vec.Vec[int]]()
	return alloc.Default().Alloc(size, align)
}

func (k *keeper) Free(p unsafe.Pointer, size, align uintptr) {
	alloc.Default().Free(p, size, align)
}

var k keeper

var log vec.Vec[int]

func note(i int) {
	for j := 0; j < 100; j++ {
		log.Push(i + j)
	}
}

func main() {
	a := alloc.Shared[vec.Vec[int]]()
	a.Push(7)
	k.ref = a
	log = vec.New[int](&k)
	s := a.Slice()
	note(1)
	println(s[0], log.Len())
}
