package main

type Stringer interface{ String() string }
type T0 struct{ p *int }
var keep *int
func (t T0) String() string { keep = t.p; return "t0" }
func call(s Stringer) string { return s.String() }
func use(v any) {}
func one() {
	type L struct{ n int }
	use(L{1})
}
func f() {
	type L struct{ T0 }
	x := 42
	println(call(L{T0{&x}}))
}
func main() { one(); f() }
