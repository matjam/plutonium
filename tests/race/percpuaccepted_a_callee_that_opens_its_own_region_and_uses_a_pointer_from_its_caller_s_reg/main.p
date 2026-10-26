package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func use(p *uint64) {
	rt.DisablePreemption()
	*p = 1
	rt.EnablePreemption()
}

func main() {
	rt.DisablePreemption()
	p := &ticks
	use(p)
	rt.EnablePreemption()
}