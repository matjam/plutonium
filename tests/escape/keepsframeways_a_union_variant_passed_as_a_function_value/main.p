package main

type Inner struct{ n int }
var keep *Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
func apply(mk func(*Inner) U, p *Inner) U { return mk(p) }
func f() {
	var in Inner
	store(apply(U.P, &in))
}
func main() { f() }
