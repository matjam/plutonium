package main

import "unsafe"

var b [10]byte
var _ = unsafe.String(&b[0], -1)

func main() {}
