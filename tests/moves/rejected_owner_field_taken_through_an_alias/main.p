package main

type L owner struct{ x int }
type N owner struct{ child owned[L] }

func main() {
	n := N{child: new(L)}
	pn := &n
	q := &*n.child
	c := take(&pn.child)
	println(q.x)
	_ = c
}
