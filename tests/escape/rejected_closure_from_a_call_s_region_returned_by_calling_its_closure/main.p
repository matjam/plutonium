package main

var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func h() { saved = own(3)() }
func main() { h() }
