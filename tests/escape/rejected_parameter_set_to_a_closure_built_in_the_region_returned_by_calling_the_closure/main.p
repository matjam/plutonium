package main

var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func swap(f func() int) func() func() int {
	f = inc(3)
	return func() func() int { return f }
}
func h() { saved = swap(nil)() }
func main() { h() }
