package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

//pu:panic
func onPanic(p *rt.Panic) {
	x86.Hlt()
}

func main() {}
