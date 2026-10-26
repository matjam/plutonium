package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	x := ticks
	rt.EnablePreemption()
	rt.DisablePreemption()
	ticks = x + 1
	rt.EnablePreemption()
}