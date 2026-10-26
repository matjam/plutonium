package main

var keepY func(*int) bool
func seq(yield func(*int) bool) { keepY = yield }
func f() {
	n := 1
	for p := range seq {
		n += *p
	}
}
func main() { f() }
