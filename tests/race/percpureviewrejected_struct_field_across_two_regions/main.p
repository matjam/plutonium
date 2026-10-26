package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

type pair struct{ a, b uint32 }

//pu:percpu
var pp pair

func main() {
	rt.DisablePreemption()
	x := pp.a
	rt.EnablePreemption()
	rt.DisablePreemption()
	pp.a = x + 1
	rt.EnablePreemption()
}