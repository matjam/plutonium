package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

//pu:panic
//pu:interrupt
func onPanic(p *rt.Panic) {
	for {
	}
}

func main() {}
