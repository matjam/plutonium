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

type Thread struct{ id int }

var boot Thread

// current is used by lone loads and stores only: allowed anywhere.
//
//pu:percpu
var current *Thread

//pu:percpu
var ticks uint64

//pu:percpu
var irqs uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	irqs++
}

func count() {
	ticks++ // outside a region
}

func countIrqs() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	irqs++ // preemption disabled is not enough: the timer handler uses irqs
}

func main() {
	current = &boot
	t := current
	_ = t
	rt.DisablePreemption()
	ticks++ // inside a region
	rt.EnablePreemption()
	count()
	countIrqs()
	_ = x86.HandlerAddr(timer)
}
