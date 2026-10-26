package main
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	ticks = x + 1
}