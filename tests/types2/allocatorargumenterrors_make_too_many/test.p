package main
import "pluto/rt"
var a rt.Allocator
type T struct{ x int }
var _ = make([]int, 1, 2, a, a)