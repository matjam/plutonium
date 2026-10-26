package main

var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func gen(n int) func(func(func() int) bool) {
	inner := inc(n)
	return func(yield func(func() int) bool) { yield(inner) }
}
func h() {
	for f := range gen(3) {
		saved = f
	}
}
func main() { h() }
