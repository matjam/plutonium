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

type pair struct{ a, b uint32 }

//pu:percpu
var pp pair

func put(p *uint64, x uint64) { *p = x }

func viaCallee() {
	var l uint64
	put(&l, ticks)
	ticks = l + 1
}

func viaPointer() {
	x := ticks
	rt.DisablePreemption()
	p := &ticks
	*p = x + 1
	rt.EnablePreemption()
}

func twoRegions() {
	rt.DisablePreemption()
	x := pp.a
	rt.EnablePreemption()
	rt.DisablePreemption()
	pp.a = x + 1
	rt.EnablePreemption()
}

func set(x uint64) { ticks = x }

func viaFuncValue() {
	g := set
	x := ticks
	g(x + 1)
}

func viaLoop() {
	for ticks != 0 {
		x86.Pause()
	}
	ticks = 1
}

func viaYield() {
	rt.DisablePreemption()
	x := ticks
	rt.Yield()
	ticks = x + 1
	rt.EnablePreemption()
}

// Accepted: the literal runs inside the region that holds the load.
func inOneRegion() {
	rt.DisablePreemption()
	x := ticks
	f := func() { ticks = x + 1 }
	f()
	rt.EnablePreemption()
}

func main() {
	inOneRegion()
	viaYield()
	viaCallee()
	viaPointer()
	twoRegions()
	viaLoop()
	viaFuncValue()
}
