package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var total uint64
	i := 0
again:
	rt.DisablePreemption()
	total += ticks
	ticks++
	rt.EnablePreemption()
	i++
	if i < 2 {
		goto again
	}
	println(total)
}