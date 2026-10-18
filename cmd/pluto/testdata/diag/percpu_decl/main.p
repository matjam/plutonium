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

func next() uint64 { return 7 }

// A constant initializer is allowed.
//
//pu:percpu
var fine uint64 = 3

// Computed at run time: package initialization has no per-CPU block yet.
//
//pu:percpu
var computed uint64 = next()

//pu:percpu
//pu:section .mydata
var placed uint64

func main() {
	println(fine, computed, placed)
}
