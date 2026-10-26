package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func count() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	x := ticks
	y := x * 2
	ticks = y + 1
}

func main() { count() }