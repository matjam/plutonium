package main

import "unsafe"

var x int
var _ = unsafe.Offsetof(x)

func main() {}
