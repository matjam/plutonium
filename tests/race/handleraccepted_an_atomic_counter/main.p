package main
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var ticks atomic.Int64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks.Add(1)
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(ticks.Load())
}