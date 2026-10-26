package main

type Inner struct{ n int }
var keep *Inner
type Stringer interface{ String() string }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func f() {
	var in Inner
	println(call(D{&in}))
}
func main() { f() }
