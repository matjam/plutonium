package main

type Stringer interface{ String() string }
type S struct{ n int }
var keepS *S
func (s *S) String() string { keepS = s; return "s" }
func first[T any](v *T) any { return any(v) }
func show[T any](v *T) string {
	if s, ok := any(v).(Stringer); ok {
		return s.String()
	}
	return ""
}
func f() {
	var n int
	_ = first(&n)
	s := S{42}
	println(show(&s))
}
func main() { f() }
