package main

type In struct{ p *int }
type Out struct {
	n int
	In
}
var g Out
func f() {
	x := 1
	g = Out{p: &x}
}
func main() { f() }
