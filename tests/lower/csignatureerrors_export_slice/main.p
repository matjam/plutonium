package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:export g
func g(b []byte) int { return len(b) }

func main() {}
