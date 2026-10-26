package main
//pu:percpu
var ticks uint64

func main() {
	var x uint64
	p := &x
	*p = ticks
	ticks = x
}