package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

func spin(n int) {
	spin(n + 1)
}

//pu:panic
func onPanic(p *rt.Panic) {
	spin(0)
}

func main() {}
