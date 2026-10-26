package main
//pu:percpu
var ticks uint64

func main() {
	f := func(v uint64) uint64 { return v }
	var g func(uint64) uint64 = f
	x := ticks
	y := g(x)
	ticks = y
}