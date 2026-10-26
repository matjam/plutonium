package main

type Stringer interface{ String() string }
type HF func() int
var keepF HF
func (h HF) String() string { keepF = h; return "hf" }
func call(s Stringer) string { return s.String() }
func f() {
	x := 42
	println(call(HF(func() int { return x })))
}
func main() { f() }
