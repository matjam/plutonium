package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

//pu:panic
func onPanic2(p *rt.Panic) {
	for {
	}
}

func main() {}
