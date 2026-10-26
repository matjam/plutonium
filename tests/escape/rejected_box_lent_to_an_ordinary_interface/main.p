package main

type N struct{ v int }
func (n *N) Get() int { return n.v }
type G interface{ Get() int }
func f() G {
	n := new(N)
	return n
}
func main() { _ = f() }
