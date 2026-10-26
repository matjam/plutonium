package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func walk(p *uint64, n int) {
	*p += 1
	if n > 0 {
		walk(p, n-1)
	}
}

func main() {
	rt.DisablePreemption()
	p := &ticks
	walk(p, 3)
	rt.EnablePreemption()
}