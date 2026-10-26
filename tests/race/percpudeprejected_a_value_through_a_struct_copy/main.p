package main
type S struct{ n, m uint64 }

//pu:percpu
var ticks uint64

func main() {
	s := S{n: ticks}
	t := s
	ticks = t.n + 1
}