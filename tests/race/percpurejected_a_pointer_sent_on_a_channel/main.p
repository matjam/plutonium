package main
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	ch := make(chan *uint64, 1)
	rt.DisablePreemption()
	ch <- &ticks
	rt.EnablePreemption()
}