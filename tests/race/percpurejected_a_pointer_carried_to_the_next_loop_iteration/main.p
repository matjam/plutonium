package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	for i := 0; i < 2; i++ {
		rt.DisablePreemption()
		if p != nil {
			*p = 1
		}
		p = &ticks
		rt.EnablePreemption()
	}
}