package main

var keep *int
func seq(yield func(*int) bool) {
	x := 42
	yield(&x)
}
func f() {
	var last *int
	for p := range seq {
		last = p
	}
	keep = last
}
func main() { f() }
