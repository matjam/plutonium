package main

type S struct{ n int }
type B struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(s Stringer) string { return s.String() }
func f() {
	var b B
	println(call((*S)(&b)))
}
func main() { f() }
