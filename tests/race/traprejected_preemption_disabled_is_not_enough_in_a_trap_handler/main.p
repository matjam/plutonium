package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }