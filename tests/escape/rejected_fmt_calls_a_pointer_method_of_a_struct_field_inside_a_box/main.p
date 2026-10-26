package main

import "pluto/fmt"
var kept *S
type S struct{ n int }
func (s *S) FmtList() (any, int) {
	kept = s
	return nil, 0
}
type Outer struct {
	a int
	s S
}
func f() {
	o := Outer{a: 1, s: S{n: 7}}
	fmt.Println(o)
}
func main() { f() }
