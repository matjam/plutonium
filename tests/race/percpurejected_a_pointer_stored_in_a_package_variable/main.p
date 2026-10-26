package main
import "pluto/rt"

//pu:percpu
var ticks uint64

var saved *uint64

func main() {
	rt.DisablePreemption()
	saved = &ticks
	rt.EnablePreemption()
}