package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func main() {
	rt.DisablePreemption()
	x := peek()
	ticks = x + 1
	rt.EnablePreemption()
}