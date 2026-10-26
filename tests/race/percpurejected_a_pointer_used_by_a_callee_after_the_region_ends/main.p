package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) { *p = 1 }

func main() {
	rt.DisablePreemption()
	p := &ticks
	rt.EnablePreemption()
	use(p)
}