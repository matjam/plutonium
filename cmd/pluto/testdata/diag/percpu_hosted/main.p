package main

// Outside kernel mode a per-CPU variable is an error.
//
//pu:percpu
var ticks uint64

func main() {
	println(ticks)
}
