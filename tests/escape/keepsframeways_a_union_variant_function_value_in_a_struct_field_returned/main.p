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
type H struct{ mk func(*Inner) U }
func g() U {
	var in Inner
	h := H{U.P}
	return h.mk(&in)
}
func main() { store(g()) }
