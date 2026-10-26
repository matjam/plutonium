package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	i := 0
again:
	rt.DisablePreemption()
	if i == 0 {
		p = &ticks
	}
	*p = 4
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}