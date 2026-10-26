package main
//pu:percpu
var ticks uint64

func main() {
	var arr [4]uint64
	arr[2] = ticks
	ticks = arr[1]
}