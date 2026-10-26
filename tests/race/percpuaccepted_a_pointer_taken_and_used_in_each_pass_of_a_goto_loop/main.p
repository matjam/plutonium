package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	i := 0
again:
	rt.DisablePreemption()
	p := &ticks
	*p += 1
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
}