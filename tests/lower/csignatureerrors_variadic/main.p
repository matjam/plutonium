package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:extern f
func f(n int, xs ...int)

func main() {}
