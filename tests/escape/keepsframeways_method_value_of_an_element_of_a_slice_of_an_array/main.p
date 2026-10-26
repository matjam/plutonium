package main

type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var arr [2]S
	sl := arr[:]
	_ = call(sl[1].visit)
}
func main() { f() }
