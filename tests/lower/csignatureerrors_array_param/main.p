package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:extern f
func f(a [4]int32)

func main() {}
