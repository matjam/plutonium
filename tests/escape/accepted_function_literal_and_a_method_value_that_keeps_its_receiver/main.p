package main

import "pluto/alloc"
type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool {
	keepS = s
	return x > 0
}
func call(f func(int) bool) bool { return f(1) }
func main() {
	s := alloc.New[S]()
	_ = call(s.visit)
	total := 0
	_ = call(func(x int) bool {
		total += x
		return true
	})
	println(total)
}
