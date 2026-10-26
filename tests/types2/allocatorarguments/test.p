package main

import (
	"pluto/rt"
	"unsafe"
)

type arena struct{}

func (*arena) Alloc(size, align uintptr) unsafe.Pointer { return nil }
func (*arena) Free(p unsafe.Pointer, size, align uintptr) {}

type T struct{ a, b int }

const c int8 = 3

func f(a rt.Allocator, ar *arena, v T) {
	var p1 owned[T] = new(T, a)
	var p2 owned[T] = new(T, ar)
	var p3 owned[T] = new(v, a)
	var p4 owned[T] = new(T{1, 2}, ar)
	var p5 owned[int] = new(42, a)
	var p6 owned[int8] = new(c, a)
	var s1 chan int = make(chan int, 2, a)
	var s2 chan int = make(chan int, ar)
	var s3 chan byte = make(chan byte, 4.0)
	var s4 chan byte = make(chan byte, c)
	_, _, _, _, _, _, _, _, _, _ = p1, p2, p3, p4, p5, p6, s1, s2, s3, s4
}
