package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var x uint64
	for i := 0; i < 3; i++ {
		rt.DisablePreemption()
		if i > 0 {
			ticks = x
		}
		x = ticks
		rt.EnablePreemption()
	}
}