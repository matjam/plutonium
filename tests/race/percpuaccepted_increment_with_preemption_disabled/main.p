package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func count() {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	ticks++
	ticks += 2
	ticks = ticks + 1
}

func main() { count() }