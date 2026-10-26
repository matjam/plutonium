package main

type Inner struct{ n int }
type U union {
	None
	P *Inner
}
var keep U
func f() {
	var in Inner
	keep = U.P(&in)
}
func main() { f() }
