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

//pu:percpu
var ticks uint64

var saved *uint64

func afterRegion() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	*p = 0 // the region has ended
}

func keep() {
	rt.DisablePreemption()
	saved = &ticks // outlives the region
	rt.EnablePreemption()
}

func remember(p *uint64) { saved = p }

func pass() {
	rt.DisablePreemption()
	remember(&ticks) // the callee keeps it
	rt.EnablePreemption()
}

func main() {
	afterRegion()
	keep()
	pass()
}
