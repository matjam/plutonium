package main

type Box struct{ p *int }
func main() {
	x := 1
	var b Box
	q := &b
	q.p = &x
}
