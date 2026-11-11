package main

// A function that asserts on an ordinary interface parameter returns what
// the caller lent to it, so the result is a view of the variable lent
// (language spec section 18.3, Views, and section 7.9). A new value for
// that variable ends the view.

type T struct{ n int }

func get(x any) *T { return x.(*T) }

func pass(x any) *T { return get(x) }

func main() {
	b := new(T)
	p := get(b)
	b = new(T)
	println(p.n)

	c := new(T)
	q := pass(c)
	c = new(T)
	println(q.n)
}
