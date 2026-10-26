package main
//pu:percpu
var a uint64

//pu:percpu
var b uint64

func main() {
	if a == 0 {
		b = 1
	}
}