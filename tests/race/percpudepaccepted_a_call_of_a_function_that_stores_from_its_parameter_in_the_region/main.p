package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func main() {
	rt.DisablePreemption()
	set(ticks + 1)
	rt.EnablePreemption()
}