package main

type N struct{ v int }
func f() *int {
	n := new(N)
	return &n.v
}
func main() { _ = f() }
