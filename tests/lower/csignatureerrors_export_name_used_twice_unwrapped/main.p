package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

//pu:export g
func g1(a, b int64) int64 { return a + b }

//pu:export g
func g2(a, b int64) int64 { return a - b }

func main() {}
