package main

import "unsafe"

var x int
var _ = unsafe.SliceData(x)

func main() {}
