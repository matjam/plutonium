package main

import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(p *[2]int) string {
	q := (*[2]S)(unsafe.Pointer(p))
	return call(&q[1])
}
func f() {
	var x [2]int
	println(g(&x))
}
func main() { f() }
