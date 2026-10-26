package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
	x := ticks
	ticks = x + 1
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }