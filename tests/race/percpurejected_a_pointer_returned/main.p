package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func get() *uint64 {
	rt.DisablePreemption()
	defer rt.EnablePreemption()
	return &ticks
}

func main() { _ = get() }