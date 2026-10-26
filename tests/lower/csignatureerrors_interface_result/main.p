package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:extern f
func f() error

func main() {}
