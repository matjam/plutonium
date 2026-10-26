package main
//pu:percpu
var ticks uint64

func main() {
	x := ticks
	println(x)
	ticks = 7
}