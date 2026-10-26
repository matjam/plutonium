package main

var saved *int
func keep(p *int) { saved = p }
func main() {
	f := keep
	x := 1
	f(&x)
}
