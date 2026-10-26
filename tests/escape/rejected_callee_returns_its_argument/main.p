package main

func id(p *int) *int { return p }
func f() *int {
	x := 1
	return id(&x)
}
func main() { _ = f() }
