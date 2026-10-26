package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &ticks
	x86.Sti()
	x86.Cli()
	*p++
	x86.Sti()
	rt.EnablePreemption()
}