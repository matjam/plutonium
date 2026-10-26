package main

var saved *int
func ptrs(n int) func(func(*int) bool) {
	return func(yield func(*int) bool) { yield(&n) }
}
func main() {
	for p := range ptrs(1) {
		saved = p
	}
}
