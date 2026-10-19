package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Cli()
		x86.Hlt()
	}
}

//pu:interrupt
func timer(f *x86.InterruptFrame) {}

//pu:interrupt  trap
func spaced(f *x86.InterruptFrame) {}

func plain(f *x86.InterruptFrame) {}

func main() {
	defer x86.InterruptGate(timer, 0x08, 0)
	defer x86.InterruptGate(plain, 0x08, 0)
	defer x86.TrapGate(nil, 0x08, 0)
	var fr x86.InterruptFrame
	spaced(&fr)
	_ = x86.InterruptGate(spaced, 0x08, 0)
}
