package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

type R struct{ s string }

//pu:export g
func (r R) g() {}

func main() {}
