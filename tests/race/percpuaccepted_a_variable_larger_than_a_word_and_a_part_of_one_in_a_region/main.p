package main
import "pluto/rt"

type Stats struct{ a, b, c uint64 }

//pu:percpu
var stats Stats

func main() {
	rt.DisablePreemption()
	stats.a++
	s := stats
	stats = Stats{s.b, s.c, s.a}
	rt.EnablePreemption()
}