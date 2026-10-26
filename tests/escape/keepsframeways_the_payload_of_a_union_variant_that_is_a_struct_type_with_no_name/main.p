package main

type Inner struct{ n int }
type U union {
	None
	S struct{ p *Inner }
}
var keep *Inner
func f() {
	var in Inner
	u := U.S{p: &in}
	switch v := u.(type) {
	case S:
		keep = v.p
	default:
	}
}
func main() { f() }
