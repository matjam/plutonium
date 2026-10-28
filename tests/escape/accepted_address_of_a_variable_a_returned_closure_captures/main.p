package main

func f(n int) (func() int, *int) {
	return func() int { return n }, &n
}
func main() {
	g, p := f(1)
	*p = 2
	println(g())
}
