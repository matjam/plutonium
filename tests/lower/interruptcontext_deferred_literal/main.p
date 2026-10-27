package main

import (
	"pluto/alloc"
	"pluto/arch/x86"
	"pluto/rt"
	"unsafe"
)

var _ alloc.Allocator
var _ unsafe.Pointer

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

type Mutex struct{ held bool }

//pu:blocking
func (m *Mutex) Lock() { m.held = true }

func (m *Mutex) TryLock() bool {
	if m.held {
		return false
	}
	m.held = true
	return true
}

//pu:blocking
func wait()

var mu Mutex

func main() { _ = x86.InterruptGate(keyboard, 0x08, 0) }

var hook func()

func slow() { wait() }

func init() { hook = slow }

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	var n int
	defer func() { n++ }()
}
