package main

func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func unwrap(n int) func() int {
	p := own(n)
	return p()
}
func main() {
	u := unwrap(10)
	println(u(), u())
}
