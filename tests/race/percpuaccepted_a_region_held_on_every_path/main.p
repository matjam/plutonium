package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	if ticks > 3 {
		ticks++
	} else {
		ticks += 2
	}
	rt.EnablePreemption()
}