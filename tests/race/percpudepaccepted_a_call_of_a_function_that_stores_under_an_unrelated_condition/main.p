package main
//pu:percpu
var ticks uint64

func reset() { ticks = 0 }

func main() {
	n := 3
	if n > 2 {
		reset()
	}
}