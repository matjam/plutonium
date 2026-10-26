package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

type pair struct{ a, b uint64 }

//pu:percpu
var stats pair

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Cli()
	stats.a++
	x86.Sti()
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	x86.Cli()
	rt.DisablePreemption()
	p := &stats
	x86.Sti()
	*p = pair{1, 2}
	rt.EnablePreemption()
}