package main
import "pluto/rt"
var a rt.Allocator
type T struct{ x int }
var _ = new(T, a, a)