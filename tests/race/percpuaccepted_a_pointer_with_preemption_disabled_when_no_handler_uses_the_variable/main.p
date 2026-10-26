package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:percpu
var other uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	other++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	rt.DisablePreemption()
	p := &ticks
	*p++
	rt.EnablePreemption()
}