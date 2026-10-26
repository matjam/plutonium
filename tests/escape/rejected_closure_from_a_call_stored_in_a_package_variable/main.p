package main

var saved func() int
func f() func() int {
	n := 0
	return func() int { n++; return n }
}
func main() { saved = f() }
