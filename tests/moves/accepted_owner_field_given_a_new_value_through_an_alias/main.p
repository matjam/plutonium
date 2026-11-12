package main

type L owner struct {
	x int
	y int
}
type N owner struct {
	child owned[L]
	other owned[L]
}

func main() {
	n := N{child: new(L), other: new(L)}
	n.child.x = 7
	pn := &n
	q := &*n.child
	println(q.x)
	pn.child = new(L)
	r := &*n.other
	pn.child = new(L)
	println(r.x)
	p := &n
	n.child = new(L)
	println(p.child.x)
	s := &n.child.y
	pn.other = new(L)
	println(*s)
}
