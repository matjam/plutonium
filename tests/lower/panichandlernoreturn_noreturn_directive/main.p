package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

//pu:noreturn
func halt()

//pu:panic
func onPanic(p *rt.Panic) {
	println(p.Message)
	halt()
}

func main() {}
