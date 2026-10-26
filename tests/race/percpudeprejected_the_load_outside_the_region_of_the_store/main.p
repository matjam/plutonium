package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	x := ticks
	rt.DisablePreemption()
	ticks = x + 1
	rt.EnablePreemption()
}