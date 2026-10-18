package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Cli()
		x86.Hlt()
	}
}

// A second handler (E2581).
//
//pu:panic
func onPanic2(p *rt.Panic) {
	for {
	}
}

type T struct{}

// A method (E2582).
//
//pu:panic
func (T) onPanic3(p *rt.Panic) {
	for {
	}
}

// Wrong parameter (E2582).
//
//pu:panic
func onPanic4(p int) {
	for {
	}
}

// A result (E2582).
//
//pu:panic
func onPanic5(p *rt.Panic) int {
	for {
	}
}

// Generic (E2582).
//
//pu:panic
func onPanic6[X any](p *rt.Panic) {
	for {
	}
}

// Also an interrupt handler (E2582).
//
//pu:panic
//pu:interrupt
func onPanic7(p *rt.Panic) {
	for {
	}
}

// Valid interrupt frame types.
//
//pu:interrupt
func i1(f unsafe.Pointer) {}

//pu:interrupt
func i2(f uintptr, c uintptr) {}

func main() {}
