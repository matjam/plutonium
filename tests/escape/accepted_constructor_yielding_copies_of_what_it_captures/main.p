package main

import "pluto/alloc"
var saved *int
func vals(p *int) func(func(*int) bool) {
	return func(yield func(*int) bool) { yield(p) }
}
func main() {
	for p := range vals(alloc.New[int]()) {
		saved = p
	}
}
