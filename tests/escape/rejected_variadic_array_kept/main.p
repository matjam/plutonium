package main

var all []*int
func keep(ps ...*int) { all = ps }
func main() {
	x := 1
	keep(&x)
}
