package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	if ticks == 0 {
		rt.DisablePreemption()
	}
	ticks++
}