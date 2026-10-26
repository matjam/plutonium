package main

var saved *int
func keep(p *int) { saved = p }
func main() {
	x := 1
	keep(&x)
}
