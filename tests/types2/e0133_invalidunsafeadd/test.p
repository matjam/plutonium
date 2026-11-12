package main

import "unsafe"

var p unsafe.Pointer
var _ = unsafe.Add(p, float64(1))

func main() {}
