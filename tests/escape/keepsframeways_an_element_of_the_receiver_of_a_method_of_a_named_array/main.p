package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
type Arr [2]S
func (a *Arr) first() string { return call(&a[1]) }
func f() {
	var arr Arr
	println(arr.first())
}
func main() { f() }
