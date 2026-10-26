package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func y() { rt.Yield() }

func main() {
	rt.DisablePreemption()
	x := ticks
	y()
	ticks = x + 1
	rt.EnablePreemption()
}