package main
//pu:percpu
var ticks uint64

func main() {
	get := func() uint64 { return ticks }
	x := get()
	ticks = x + 1
}