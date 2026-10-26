package main
//pu:percpu
var ticks uint64

func main() {
	a := ticks
	b := a*2 + 3
	c := b >> 1
	ticks = c
}