package main

type S struct{ n int }
type B struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var b B
	_ = call((*S)(&b).visit)
}
func main() { f() }
