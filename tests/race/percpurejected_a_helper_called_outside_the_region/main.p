package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func bump() { ticks++ }

func main() {
	rt.DisablePreemption()
	bump()
	rt.EnablePreemption()
	bump()
}