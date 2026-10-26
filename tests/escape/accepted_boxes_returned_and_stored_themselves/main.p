package main

type N struct{ v int }
var G owned[N]
var S shared[N]
func f() owned[N] {
	n := new(N)
	n.v = 1
	return n
}
func main() {
	G = f()
	S = nil
	p := (*N)(G)
	_ = p.v
}
