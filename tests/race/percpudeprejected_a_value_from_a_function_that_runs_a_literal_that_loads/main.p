package main
//pu:percpu
var ticks uint64

func apply(f func() uint64) uint64 { return f() }

func main() {
	x := apply(func() uint64 { return ticks })
	ticks = x + 1
}