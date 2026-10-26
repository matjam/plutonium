package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

type T struct{ B []int }

//pu:extern f
func f(t T)

func main() {}
