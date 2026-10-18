package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

// The panic handler prints and then returns, which it must not.
//
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	x86.Hlt()
}

type Mutex struct{ held bool }

//pu:blocking
func (m *Mutex) Lock() { m.held = true }

type Locker interface{ Lock() }

var cfgMutex Mutex

var cfgLock Locker = &cfgMutex

func readConfig() { cfgLock.Lock() }

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	readConfig()
}

func main() {
	_ = x86.InterruptGate(keyboard, 0x08, 0)
}
