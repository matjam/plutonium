package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	ticks++
	rt.EnablePreemption()
	ticks++
}