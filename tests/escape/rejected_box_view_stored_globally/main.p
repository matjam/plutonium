package main

type N struct{ v int }
var G *N
func f() {
	n := new(N)
	G = (*N)(n)
}
func main() { f() }
