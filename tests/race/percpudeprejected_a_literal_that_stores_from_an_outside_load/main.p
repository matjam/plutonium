package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func within(f func()) {
	rt.DisablePreemption()
	f()
	rt.EnablePreemption()
}

func main() {
	x := ticks
	within(func() { ticks = x + 1 })
}