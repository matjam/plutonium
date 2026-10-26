package main
//pu:percpu
var ticks uint64

func main() {
	p := &ticks
	*p = 1
}