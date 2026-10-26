package main
//pu:percpu
var a uint64

//pu:percpu
var b uint64

func main() {
	x := a
	b = x + 1
}