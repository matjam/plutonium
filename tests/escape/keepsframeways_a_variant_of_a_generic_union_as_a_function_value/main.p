package main

type Inner struct{ n int }
var keep *Inner
type Opt[T any] union {
	None
	Some T
}
func store(o Opt[*Inner]) {
	if p, ok := o.(Opt[*Inner].Some); ok {
		keep = p
	}
}
func f() {
	var in Inner
	mk := Opt[*Inner].Some
	store(mk(&in))
}
func main() { f() }
