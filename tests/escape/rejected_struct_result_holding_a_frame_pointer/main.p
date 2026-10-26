package main

type R struct {
	n int
	p *int
}
func f() R {
	x := 1
	return R{n: 1, p: &x}
}
func main() { _ = f() }
