package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func ping(p *uint64, n int) {
	rt.DisablePreemption()
	if p == nil {
		p = &ticks
	}
	*p = 5
	rt.EnablePreemption()
	if n > 0 {
		pong(p, n-1)
	}
}

func pong(p *uint64, n int) { ping(p, n) }

func main() {
	ping(nil, 3)
}