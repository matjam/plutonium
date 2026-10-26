package main

type Stringer interface{ String() string }
type Named struct{ name string }
var last *Named
func (n *Named) String() string {
	last = n
	return "named"
}
type Wide struct{ a, b, c int }
func (w Wide) String() string { return "wide" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func main() {
	var n Named
	println(call(&n), call(Wide{1, 2, 3}))
}
