package main

func f() (p *int) {
	x := 2
	p = &x
	return
}
func main() { _ = f() }
