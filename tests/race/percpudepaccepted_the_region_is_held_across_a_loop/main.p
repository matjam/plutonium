package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	for i := 0; i < 3; i++ {
		x := ticks
		ticks = x + 1
	}
	rt.EnablePreemption()
}