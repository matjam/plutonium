package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func main() {
	x := ticks
	rt.DisablePreemption()
	p := &ticks
	*p = x + 1
	rt.EnablePreemption()
}