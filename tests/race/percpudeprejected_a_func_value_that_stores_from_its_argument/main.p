package main
//pu:percpu
var ticks uint64

func main() {
	set := func(v uint64) { ticks = v }
	set(ticks + 1)
}