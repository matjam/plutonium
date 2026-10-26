package main

import "unsafe"

var _ unsafe.Pointer

type S struct{ A int }

type P struct{ A, B int32 }

//pu:export g
func g1(p P) int { return 0 }

//pu:export g
func g2(p P) int { return 1 }

func main() {}
