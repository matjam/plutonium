package main
type Stats struct{ a uint64 }

func (s *Stats) bump() { s.a++ }

//pu:percpu
var stats Stats

func main() {
	stats.bump()
}