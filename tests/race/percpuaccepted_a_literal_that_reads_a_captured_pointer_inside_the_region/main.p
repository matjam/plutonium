package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	rt.DisablePreemption()
	p := &ticks
	f := func() { *p += 1 }
	f()
	f()
	rt.EnablePreemption()
}