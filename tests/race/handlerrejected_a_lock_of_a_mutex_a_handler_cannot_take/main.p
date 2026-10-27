package main
import (
	"pluto/arch/x86"
	"pluto/sync"
)

var (
	mu sync.Mutex
	n  int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	n = 1
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	mu.Lock()
	n = 2
	mu.Unlock()
}