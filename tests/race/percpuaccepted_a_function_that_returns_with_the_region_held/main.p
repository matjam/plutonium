package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func enter() { rt.DisablePreemption() }

func main() {
	enter()
	ticks++
	rt.EnablePreemption()
}