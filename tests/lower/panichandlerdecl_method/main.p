package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

type T struct{}

//pu:panic
func (T) onPanic(p *rt.Panic) {
	for {
	}
}

func main() {}
