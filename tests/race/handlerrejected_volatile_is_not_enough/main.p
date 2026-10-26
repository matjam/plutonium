package main
import "pluto/arch/x86"

var ticks volatile[uint64]

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks = ticks + 1
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(ticks)
}