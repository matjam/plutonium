package main
//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func main() {
	x := peek()
	ticks = x + 1
}