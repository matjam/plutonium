package main

import (
	"pluto/arch/x86"
	"pluto/rt"
)

var (
	mu rt.Lock
	n  int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	mu.Unlock() // the interrupted code may hold mu; the handler does not
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	mu.Lock()
	n++
	mu.Unlock()
	println(n)
}
