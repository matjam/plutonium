package main
//pu:percpu
var ticks uint64

//pu:extern ext
func ext(v uint64) uint64

func main() {
	for i := 0; i < 2; i++ {
		ticks = ext(ticks)
	}
}