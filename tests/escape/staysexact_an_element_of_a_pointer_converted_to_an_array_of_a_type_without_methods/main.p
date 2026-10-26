package main

import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(s Stringer) string { return s.String() }
func sum(p *int) int { return *p + 1 }
func f() {
	var x [2]S
	q := (*[2]int)(unsafe.Pointer(&x))
	println(sum(&q[1]))
}
func main() { f() }
