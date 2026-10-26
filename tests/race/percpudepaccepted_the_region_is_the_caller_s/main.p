package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func bump() {
	x := ticks
	ticks = x + 1
}

func main() {
	rt.DisablePreemption()
	bump()
	rt.EnablePreemption()
}