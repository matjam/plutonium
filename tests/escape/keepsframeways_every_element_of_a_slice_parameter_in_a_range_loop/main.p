package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func each(items []S) string {
	out := ""
	for i := range items {
		var s Stringer = &items[i]
		out += s.String()
	}
	return out
}
func f() {
	items := [3]S{}
	println(each(items[:]))
}
func main() { f() }
