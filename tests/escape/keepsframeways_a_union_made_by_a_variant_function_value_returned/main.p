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
func g() U {
	var in Inner
	mk := U.P
	return mk(&in)
}
func main() { store(g()) }
