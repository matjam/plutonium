package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 3; i++ {
		rt.DisablePreemption()
		x := ticks
		ticks = x + 1
		rt.EnablePreemption()
	}
}