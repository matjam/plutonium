package main

import "pluto/alloc"
type Stringer interface{ String() string }
type Named struct{ name string }
var last *Named
func (n *Named) String() string {
	last = n
	return "named"
}
// A value method of Wide is called on a Wide box, which the pointer method of
// Named never gets: the receiver type narrows what the type assertion left.
type Wide struct{ a, b, c int }
func (w Wide) String() string { return "wide" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func main() {
	n := alloc.New[Named]()
	println(call(n))
	println(call(Wide{1, 2, 3}))
}
