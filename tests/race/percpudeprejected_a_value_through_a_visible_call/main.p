package main
//pu:percpu
var ticks uint64

func inc(v uint64) uint64 { return v + 1 }

func main() {
	x := ticks
	ticks = inc(x)
}