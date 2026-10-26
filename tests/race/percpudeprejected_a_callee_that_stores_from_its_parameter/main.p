package main
//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func main() {
	set(ticks + 1)
}