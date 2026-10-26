package main

import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
type B struct{ a, b S }
func f() {
	var x [2]int
	q := (*B)(unsafe.Pointer(&x))
	println(call(&q.b))
}
func main() { f() }
