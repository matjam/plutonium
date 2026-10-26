package main
type Pair struct{ a, b uint32 }

//pu:percpu
var pair Pair

func main() {
	println(pair.a)
}