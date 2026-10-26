package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

func stop(n int) {
	if n > 0 {
		for {
		}
	}
}

//pu:panic
func onPanic(p *rt.Panic) {
	stop(p.CPU)
}

func main() {}
