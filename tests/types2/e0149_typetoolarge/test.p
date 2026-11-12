package main

import "unsafe"

type E [1 << 31 - 1]int
var a [1 << 31]E
var _ = unsafe.Sizeof(a)

func main() {}
