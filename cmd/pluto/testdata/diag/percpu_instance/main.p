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

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

// A pointer from one pass of a goto loop used in the next.
func again() {
	var p *uint64
	i := 0
loop:
	x86.Cli()
	if i == 0 {
		p = &ticks
	}
	_ = p
	x86.Sti()
	i++
	if i < 2 {
		goto loop
	}
}

// A pointer from the caller's region used in the recursive call.
func rec(p *uint64, n int) {
	x86.Cli()
	if p == nil {
		p = &ticks
	}
	*p = 5
	x86.Sti()
	if n > 0 {
		rec(p, n-1)
	}
}

// A literal called twice keeps the pointer in a captured variable.
func twice() {
	var p *uint64
	f := func() {
		x86.Cli()
		if p == nil {
			p = &ticks
		}
		x86.Sti()
	}
	f()
	f()
}

// Interrupts are enabled again when the pointer is used.
func enabled() {
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	*p++
	rt.EnablePreemption()
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	again()
	rec(nil, 2)
	twice()
	enabled()
}
