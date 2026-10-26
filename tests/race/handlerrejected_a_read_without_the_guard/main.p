package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var (
	mu    rt.Lock
	state int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	mu.Lock()
	state = 1
	mu.Unlock()
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(state)
}