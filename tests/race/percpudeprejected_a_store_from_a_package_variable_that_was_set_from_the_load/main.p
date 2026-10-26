package main
var saved uint64

//pu:percpu
var ticks uint64

func main() {
	saved = ticks
	ticks = saved + 1
}