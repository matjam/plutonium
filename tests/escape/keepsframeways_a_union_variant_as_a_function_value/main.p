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
func f() {
	var in Inner
	mk := U.P
	store(mk(&in))
}
func main() { f() }
