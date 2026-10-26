package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ x86.InterruptFrame
var _ rt.Panic

func stop() {
	for {
		x86.Cli()
		x86.Hlt()
	}
}

func die(n int) {
	if n > 0 {
		stop()
	}
	stop()
}

//pu:panic
func onPanic(p *rt.Panic) {
	println(p.Message)
	die(p.CPU)
}

func main() {}
