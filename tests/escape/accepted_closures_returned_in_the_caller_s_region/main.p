package main

func f() func() int {
	n := 0
	return func() int { n++; return n }
}
type C struct{ n int }
func (c C) Get() int { return c.n }
func g(c C) func() int { return c.Get }
func h(k int) func() int {
	if k < 0 {
		panic("k")
	}
	return f()
}
func main() {
	a, b, c := f(), g(C{1}), h(2)
	println(a(), b(), c())
}
