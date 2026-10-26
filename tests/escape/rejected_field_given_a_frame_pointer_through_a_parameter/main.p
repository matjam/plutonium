package main

type N struct{ next *N }
func link(a, b *N) { a.next = b }
var G *N
func keep(n *N) { G = n.next }
func main() {
	var x, y N
	link(&x, &y)
	keep(&x)
}
