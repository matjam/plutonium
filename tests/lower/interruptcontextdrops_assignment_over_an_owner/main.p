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

type Conn owner struct{ id int }

func (c *Conn) Drop() { wait() }

func (c *Conn) Touch() int { return c.id }

func mk() Conn { return Conn{id: 1} }

func use(c *Conn) {}

type slowFree struct{}

func (slowFree) Alloc(size, align uintptr) unsafe.Pointer { return nil }

func (slowFree) Free(p unsafe.Pointer, size, align uintptr) { wait() }

type Plain owner struct{ n int }

type Pair owner struct{ a, b Conn }

func useP(p *Pair) {}

func eat(c Conn) {}

var name string

func outb()

var flag bool

//pu:blocking
func spinUntilSet() {
	for !flag {
	}
}

var setupFn = setup

func setup() { alloc.SetDefault(slowFree{}) }

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	c := mk()
	use(&c)
	c = mk()
	use(&c)
	x86.Hlt()
}
