package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

// A function without a body is taken at its declaration: this one does not
// wait, and calling it in a handler is allowed.
func outb(port uint16, v uint8)

type Mutex struct{ held bool }

//pu:blocking
func (m *Mutex) Lock() { m.held = true }

var devMutex Mutex

// Conn's Drop takes a lock that waits. No call of it is written in the
// handler below; the compiler adds it where the value is dropped.
type Conn owner struct{ id int }

func (c *Conn) Drop() { devMutex.Lock() }

func open() Conn { return Conn{id: 1} }

func use(c *Conn) {}

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	outb(0x20, 0x20)
	c := open()
	use(&c)
}

func main() {
	_ = x86.HandlerAddr(keyboard)
}
