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

var idle, next Thread

//pu:percpu
var current *Thread

//pu:percpu
var ticks uint64

func get() *Thread { return current }

func set(t *Thread) { current = t }

func tick() {
	x := ticks
	ticks = x + 1 // a store of a value computed from the load
}

func pick() {
	if current == &idle {
		current = &next // a store controlled by the load
	}
}

func swap() {
	t := get()
	set(t) // the callee stores a value that came from a load
}

func safe() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	x := ticks
	ticks = x + 1 // the load and the store are in one region
	if current == &idle {
		current = &next
	}
}

func independent() {
	t := current // two lone accesses that do not depend on each other
	_ = t
	current = &idle
}

func main() {
	tick()
	pick()
	swap()
	safe()
	independent()
}
