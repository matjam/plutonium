package main

// What is allocated through the default allocator while a local arena is
// installed lives in the arena, and may not outlive it (language spec
// section 7.1).

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/vec"
)

var saved string

// A string built while the arena is the default.
func name(n int) string {
	var mem [256]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore()
	return fmt.Sprintf("item %d", n)
}

// A vec.Vec that takes alloc.Default() as its allocator.
func items() vec.Vec[int] {
	var mem [1024]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore()
	v := vec.New[int]()
	v.Push(1)
	return v
}

// A concatenation kept in a package variable.
func keep(s string) {
	var mem [256]byte
	a := arena.New(mem[:])
	r := alloc.Use(&a)
	saved = s + "!"
	r.Restore()
}

// Allocations that stay inside the window are fine, and so are those made
// from an allocator passed explicitly.
func local() vec.Vec[int] {
	heap := alloc.Default()
	var mem [256]byte
	a := arena.New(mem[:])
	defer alloc.Use(&a).Restore()
	s := fmt.Sprintf("%d", 3)
	v := vec.New[int](heap)
	v.Push(len(s))
	return v
}

func main() {
	_ = name(1)
	_ = items()
	keep("x")
	_ = local()
}
