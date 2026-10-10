package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

// Explicit heap allocation. Nothing allocates unless asked: new, make and
// the alloc package take memory from the default allocator (a slab
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

	// new and make use the same allocator.
	pt := new(Point)
	defer alloc.Free(pt)
	pt.X, pt.Y = 3, 4
	fmt.Printf("point=%d,%d\n", pt.X, pt.Y)

	buf := make([]byte, 0, 16)
	buf = append(buf, "hello"...)
	fmt.Printf("buf=%s len=%d cap=%d\n", buf, len(buf), cap(buf))
}
