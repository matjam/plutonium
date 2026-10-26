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

type slowAlloc struct{}

func (slowAlloc) Alloc(size, align uintptr) unsafe.Pointer {
	wait()
	return nil
}

func (slowAlloc) Free(p unsafe.Pointer, size, align uintptr) {}

func init() { alloc.SetDefault(slowAlloc{}) }

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	_ = new(int)
}
