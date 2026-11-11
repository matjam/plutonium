package main

// The view a function returns out of an interface parameter by assertion
// lasts until the variable lent gets a new value, so uses before that are
// accepted, and a function that does not assert returns no view.

type T struct{ n int }

func get(x any) *T { return x.(*T) }

var g T

func fresh(x any) *T { return &g }

func main() {
	b := new(T)
	p := get(b)
	println(p.n)
	b = new(T)
	println(b.n)

	c := new(T)
	q := fresh(c)
	c = new(T)
	println(q.n, c.n)
}
