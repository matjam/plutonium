package main
type Stats struct{ a, b, c uint64 }

//pu:percpu
var stats Stats

func main() {
	s := stats
	println(s.a)
}