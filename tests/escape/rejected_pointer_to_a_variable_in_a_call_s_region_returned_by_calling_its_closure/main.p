package main

var saved *int
func mk() func() *int {
	x := 3
	p := &x
	return func() *int {
		x++
		return p
	}
}
func h() { saved = mk()() }
func main() { h() }
