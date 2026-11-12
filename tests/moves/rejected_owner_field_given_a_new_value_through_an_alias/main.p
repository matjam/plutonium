package main

type L owner struct{ x int }
type N owner struct{ child owned[L] }

func main() {
	n := N{child: new(L)}
	n.child.x = 7
	pn := &n
	q := &*n.child
	pn.child = new(L)
	println(q.x)
}
