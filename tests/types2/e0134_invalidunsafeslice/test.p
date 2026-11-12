package main

import "unsafe"

var x int
var _ = unsafe.Slice(x, 1)

func main() {}
