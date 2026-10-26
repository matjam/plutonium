package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:export g
func g(xs ...int) {}

func main() {}
