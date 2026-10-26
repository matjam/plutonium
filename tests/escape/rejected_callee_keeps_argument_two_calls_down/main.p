package main

var saved *int
func keep(p *int) { saved = p }
func relay(q *int) { keep(q) }
func main() {
	x := 1
	relay(&x)
}
