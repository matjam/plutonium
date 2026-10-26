package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

type T struct{ N int; S string }

//pu:extern f
func f() T

func main() {}
