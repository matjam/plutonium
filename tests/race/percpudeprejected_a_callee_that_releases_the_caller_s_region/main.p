package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func reopen() {
	rt.EnablePreemption()
	rt.DisablePreemption()
}

func main() {
	rt.DisablePreemption()
	x := ticks
	reopen()
	ticks = x + 1
	rt.EnablePreemption()
}