package main

var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func get(f func() func() int) func() int { return f() }
func h() { saved = get(own(3)) }
func main() { h() }
