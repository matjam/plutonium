package main

type N struct{ p *int }
func set(pp **int, v *int) { *pp = v }
var G *int
func main() {
	var n N
	x := 1
	set(&n.p, &x)
	G = n.p
}
