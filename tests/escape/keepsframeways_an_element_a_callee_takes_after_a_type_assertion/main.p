package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(a any) string {
	if p, ok := a.(*[2]S); ok {
		return call(&p[1])
	}
	return ""
}
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }
