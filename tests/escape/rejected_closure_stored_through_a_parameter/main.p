package main

type H struct{ f func() int }
func set(h *H) {
	n := 1
	h.f = func() int { return n }
}
func main() { set(&H{}) }
