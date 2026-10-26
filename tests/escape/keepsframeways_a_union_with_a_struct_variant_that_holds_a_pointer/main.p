package main

type Inner struct{ n int }
type U union {
	None
	S struct{ p *Inner }
}
var keep U
func f() {
	var in Inner
	keep = U.S{p: &in}
}
func main() { f() }
