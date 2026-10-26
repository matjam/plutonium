package main

func mk(p *int) func() int {
	x := 3
	q := &x
	return func() int { return *p + *q + x }
}
func main() {
	n := 1
	println(mk(&n)())
}
