package main
//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 5; i++ {
		if ticks > 3 {
			break
		}
	}
	ticks = 0
}