package main
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	x = 5
	ticks = x
}