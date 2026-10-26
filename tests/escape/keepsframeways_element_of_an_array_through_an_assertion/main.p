package main

type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func f() {
	var arr [2]S
	println(call(&arr[1]))
}
func main() { f() }
