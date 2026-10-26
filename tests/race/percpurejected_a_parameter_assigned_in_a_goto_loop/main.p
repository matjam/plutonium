package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func loop(p *uint64) {
	i := 0
again:
	rt.DisablePreemption()
	if i == 1 {
		*p = 1
	}
	p = &ticks
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}

func main() { loop(nil) }