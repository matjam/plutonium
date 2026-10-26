package main

type Stringer interface{ String() string }
type HF func() int
type C struct{ n int }
func (c *C) get() int { return c.n }
var keepF HF
func (h HF) String() string { keepF = h; return "hf" }
func call(s Stringer) string { return s.String() }
func f() {
	var c C
	println(call(HF(c.get)))
}
func main() { f() }
