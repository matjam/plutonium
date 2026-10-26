package main

type Inner struct{ n int }
var keep *Inner
var global Inner
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
	mk := U.P
	store(mk(&global))
}
func main() { f() }
