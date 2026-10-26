package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
type Plain struct{ n int }
func (p *Plain) String() string { return "plain" }
func call(s Stringer) string { return s.String() }
func f() {
	var p Plain
	println(call(&p))
}
func main() { f() }
