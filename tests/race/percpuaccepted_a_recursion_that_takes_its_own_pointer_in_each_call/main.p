package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func rec(n int) {
	rt.DisablePreemption()
	p := &ticks
	*p += 1
	rt.EnablePreemption()
	if n > 0 {
		rec(n - 1)
	}
}

func main() { rec(3) }