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

// Valid handlers.
//
//pu:interrupt
func timer(f *x86.InterruptFrame) {}

//pu:interrupt
func fault(f *x86.InterruptFrame, code uint64) {}

type T struct{}

// A method cannot be a handler (E2570).
//
//pu:interrupt
func (T) method(f *x86.InterruptFrame) {}

// Generic (E2570).
//
//pu:interrupt
func generic[X any](f *x86.InterruptFrame) {}

// Also inline (E2570).
//
//pu:inline
//pu:interrupt
func inlined(f *x86.InterruptFrame) {}

// No body (E2570).
//
//pu:interrupt
func nobody(f *x86.InterruptFrame)

// Wrong parameters (E2571).
//
//pu:interrupt
func noparams() {}

//pu:interrupt
func threeparams(f *x86.InterruptFrame, a, b uint64) {}

//pu:interrupt
func result(f *x86.InterruptFrame) int { return 0 }

//pu:interrupt
func badframe(f int) {}

//pu:interrupt
func badcode(f *x86.InterruptFrame, c string) {}

func plain() {}

//pu:interrupt trap
func reentrant(f *x86.InterruptFrame) {}

func main() {
	timer(nil)          // E2573: call
	defer fault(nil, 0) // E2573: defer
	f := timer          // E2573: func value
	_ = f
	_ = x86.InterruptGate(timer, 0x08, 0)
	_ = x86.InterruptGate(plain, 0x08, 0) // E2572
	g := timer
	_ = x86.InterruptGate(g, 0x08, 0) // E2572
	_ = x86.TrapGate(reentrant, 0x08, 0)
	_ = x86.TrapGate(timer, 0x08, 0)          // E2572: a plain handler needs an interrupt gate
	_ = x86.InterruptGate(reentrant, 0x08, 0) // E2572: a trap handler needs a trap gate
	h := x86.InterruptGate                    // E2572: the constructors take a name
	_ = h
}
