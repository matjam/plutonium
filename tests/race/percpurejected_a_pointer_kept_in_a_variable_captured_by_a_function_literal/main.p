package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	f := func() {
		rt.DisablePreemption()
		if p == nil {
			p = &ticks
		}
		*p = 15
		rt.EnablePreemption()
	}
	f()
	f()
}