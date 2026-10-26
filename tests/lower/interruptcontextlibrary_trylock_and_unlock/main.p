package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync"
	"pluto/time"
)

var (
	mu sync.Mutex
	wg sync.WaitGroup
	ch = make(chan int, 1)
)

func work() {}

func readConfig() {
	mu.Lock()
	mu.Unlock()
}

var _ = rt.MinStackSize
var _ = time.Second

//pu:interrupt
func keyboard(f *x86.InterruptFrame) {
	if mu.TryLock() {
		mu.Unlock()
	}
}

func main() { _ = x86.InterruptGate(keyboard, 0x08, 0) }
