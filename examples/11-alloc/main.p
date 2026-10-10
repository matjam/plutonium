package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/vec"
)

// Explicit heap allocation. Nothing allocates unless asked: new, vec.Vec
// and the alloc package take memory from the default allocator (a slab
// allocator in hosted programs), and memory is returned explicitly.

type Point struct{ X, Y int }

func main() {
	p := alloc.New[int64]()
	*p = 42
	fmt.Printf("heap value=%d\n", *p)
	alloc.Free(p)

	// Freed slots are reused.
	q := alloc.New[int64]()
	fmt.Printf("reused slot is zeroed: %d\n", *q)
	alloc.Free(q)

	// new and vec.Vec use the same allocator.
	pt := new(Point)
	defer alloc.Free(pt)
	pt.X, pt.Y = 3, 4
	fmt.Printf("point=%d,%d\n", pt.X, pt.Y)

	// A vec.Vec grows from the allocator; a slice cannot grow.
	buf := vec.WithCapacity[byte](16)
	defer buf.Free()
	vec.AppendString(&buf, "hello")
	fmt.Printf("buf=%s len=%d cap=%d\n", buf.Slice(), buf.Len(), buf.Cap())
}
