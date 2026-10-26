package main

type Inner struct{ n int }
type U union {
	None
	P *Inner
}
var keep *Inner
func f() {
	var in Inner
	u := U.P(&in)
	switch v := u.(type) {
	case P:
		keep = v
	default:
	}
}
func main() { f() }
