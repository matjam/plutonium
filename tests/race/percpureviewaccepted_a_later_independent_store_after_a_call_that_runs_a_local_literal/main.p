package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func a() {
	rt.DisablePreemption()
	x := ticks
	f := func() { ticks = x + 1 }
	f()
	rt.EnablePreemption()
}

func reset() { ticks = 0 }

func main() {
	a()
	reset()
}