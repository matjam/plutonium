package main

// The address of a variable a returned closure captures (spec section
// 11.5) is valid as long as the region of the call: the closure and the
// pointer see the same variable.

func mk() (func() int, *int) {
	x := 1
	g := func() int { return x }
	return g, &x
}

// A function that returns a closure passes its region down.
func pass() (func() int, *int) {
	g, p := mk()
	*p += 10
	return g, p
}

type pair struct {
	f func() int
	p *int
}

func both(n int) pair {
	x := n
	return pair{f: func() int { return x * 2 }, p: &x}
}

// The address stored in a variable the closure captures, which is in the
// region too.
func counter() func() int {
	x := 1
	var q *int
	g := func() int {
		*q += 1
		return x
	}
	q = &x
	return g
}

func main() {
	g, p := mk()
	println(g(), *p)
	*p = 5
	println(g(), *p)

	for i := 0; i < 3; i++ {
		g, p := pass()
		*p += i
		println(g(), *p)
	}

	s := both(3)
	*s.p = 4
	println(s.f())

	var last *int
	for i := 0; i < 3; i++ {
		_, p := mk()
		*p = i + 7
		last = p
		break
	}
	println(*last)

	c := counter()
	println(c(), c())
}
