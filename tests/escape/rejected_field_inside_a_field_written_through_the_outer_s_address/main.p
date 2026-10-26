package main

type In struct{ p *int }
type Out struct{ in In }
func set(i *In, v *int) { i.p = v }
var G *int
func main() {
	var o Out
	x := 1
	set(&o.in, &x)
	G = o.in.p
}
