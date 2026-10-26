package main
//pu:percpu
var ticks uint64

func peek() uint64 { return ticks }

func twice() uint64 { return peek() * 2 }

func main() {
	x := twice()
	ticks = x
}