package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) { ticks = 0 }

func work() {
	ticks++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	task work()
	rt.Yield()
}