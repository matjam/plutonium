package main

// The type of a constant must not be an owner type (spec section 10.6),
// whether it is written or comes from the value.

type F owner int32

type Frame owner uintptr

const c F = 1

const (
	a Frame = iota
	b
)

const d = F(2)

func main() {}
