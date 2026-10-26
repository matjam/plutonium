package main
import "pluto/rt"

type Stats struct{ a, b uint64 }

//pu:percpu
var stats Stats

func add(s *Stats) { s.a++ }

func (s *Stats) bump() { s.b++ }

func main() {
	rt.DisablePreemption()
	p := &stats
	p.a++
	add(p)
	stats.bump()
	add(&stats)
	rt.EnablePreemption()
}