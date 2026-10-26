package main

func id(p *int) *int { return p }
func main() {
	x := 1
	q := id(&x)
	*q = 2
}
