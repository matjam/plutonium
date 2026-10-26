package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func rec(p *uint64, n int) {
	rt.DisablePreemption()
	if p == nil {
		p = &ticks
	}
	*p = 5
	rt.EnablePreemption()
	if n > 0 {
		rec(p, n-1)
	}
}

func main() {
	rec(nil, 3)
}