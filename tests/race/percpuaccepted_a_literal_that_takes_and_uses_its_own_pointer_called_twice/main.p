package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	f := func() {
		rt.DisablePreemption()
		p := &ticks
		*p += 1
		rt.EnablePreemption()
	}
	f()
	f()
}