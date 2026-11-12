package main

// A box field of a local owner given a new value drops the old box: a
// view into its object is stale (language spec section 18.3).

type L owner struct{ x int }
type N owner struct{ c owned[L] }

func main() {
	var n N
	n.c = new(L)
	n.c.x = 7
	p := &n.c.x
	n.c = new(L)
	println(*p)
}
