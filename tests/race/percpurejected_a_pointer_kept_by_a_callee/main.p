package main
import "pluto/rt"

//pu:percpu
var ticks uint64

var keep *uint64

func remember(p *uint64) { keep = p }

func main() {
	rt.DisablePreemption()
	remember(&ticks)
	rt.EnablePreemption()
}