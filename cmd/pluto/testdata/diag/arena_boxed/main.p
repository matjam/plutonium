package main

// Text built in a local arena is a view of the arena (language spec section
// 18.8, Arenas). Converting it to an interface puts a copy in the frame that
// is released when the function returns, after the arena is dropped, and
// returning it outlives the arena. Writing its bytes, or text from an
// allocator that outlives the arena, is fine.

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/os"
	"pluto/strings"
)

// A Builder started with the arena as its allocator.
func builder() int {
	a := arena.NewFixed(16 << 10)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	fmt.Println(out.String())
	return 1
}

// Text made while the arena is the default allocator.
func window(n int) int {
	a := arena.NewFixed(16 << 10)
	defer alloc.Use(&a).Restore()
	s := fmt.Sprintf("n=%d", n)
	fmt.Println(s)
	return 1
}

// Returning the text outlives the arena.
func leak() string {
	a := arena.NewFixed(16 << 10)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	return out.String()
}

// Writing the bytes, taking the length, and text from the heap are fine.
func fine() int {
	a := arena.NewFixed(16 << 10)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	os.Stdout.Write(out.Bytes())
	var heap strings.Builder
	defer heap.Free()
	heap.WriteString("world")
	fmt.Println(heap.String(), len(out.String()))
	return 1
}

func main() {
	_ = builder()
	_ = window(1)
	_ = leak()
	_ = fine()
}
