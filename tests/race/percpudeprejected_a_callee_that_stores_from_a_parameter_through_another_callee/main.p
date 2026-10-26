package main
//pu:percpu
var ticks uint64

func set(v uint64) { ticks = v }

func setTwice(v uint64) { set(v * 2) }

func main() {
	x := ticks
	setTwice(x)
}