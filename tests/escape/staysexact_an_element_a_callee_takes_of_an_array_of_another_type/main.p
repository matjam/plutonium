package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
type Plain struct{ n int }
func (p *Plain) String() string { return "plain" }
func call(s Stringer) string { return s.String() }
func g(p *[2]Plain) string { return call(&p[1]) }
func f() {
	var arr [2]Plain
	println(g(&arr))
}
func main() { f() }
