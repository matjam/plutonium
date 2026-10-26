package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	rt.DisablePreemption()
	*p = 1
	rt.EnablePreemption()
}