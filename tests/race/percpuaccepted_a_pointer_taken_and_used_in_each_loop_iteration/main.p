package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 2; i++ {
		rt.DisablePreemption()
		p := &ticks
		*p += 1
		rt.EnablePreemption()
	}
}