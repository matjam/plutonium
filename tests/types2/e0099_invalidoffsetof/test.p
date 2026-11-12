package main

import "unsafe"

type T struct { f int }
type S struct { *T }
var s S
var _ = unsafe.Offsetof(s.f)

func main() {}
