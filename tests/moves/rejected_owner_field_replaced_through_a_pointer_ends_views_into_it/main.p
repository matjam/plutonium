package main

// A box field given a new value through a pointer to the local that
// holds it drops the old box: a view into its object is stale (language
// spec section 18.3).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	var n N
	n.c = new(L)
	n.c.x = 7
	p := &n
	q := &p.c.x
	p.c = new(L)
	println(*q)
}
