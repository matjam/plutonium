package main

// The address of a variable a returned closure captures (spec section
// 11.5): the variable is in the region of the call, a block of the
// caller's frame, so the address is a view of that frame.

func mk() (func() int, *int) {
	x := 1
	g := func() int { return x }
	return g, &x
}

// Fine: a function that returns a closure passes its own region down, so
// it may return the address too.
func pass() (func() int, *int) {
	g, p := mk()
	*p += 10
	return g, p
}

// h returns no closure, so it takes no region: the call's region is in h's
// frame.
func h() *int {
	_, p := mk()
	return p
}

// The same, through a copy.
func h2() *int {
	_, p := mk()
	q := p
	return q
}

// The call refills its region on every iteration.
func KeepLast() int {
	var keep *int
	for i := 0; i < 3; i++ {
		_, p := mk()
		*p = i
		keep = p
	}
	return *keep
}

type holder struct {
	p *int
}

// The same, in a field of a variable declared outside the loop.
func KeepField() int {
	var s holder
	for i := 0; i < 3; i++ {
		_, p := mk()
		s.p = p
	}
	return *s.p
}

// A goto back to a label before the call makes a loop.
func Again() int {
	var keep *int
	total := 0
	i := 0
again:
	if keep != nil {
		total += *keep
	}
	_, p := mk()
	keep = p
	i++
	if i < 3 {
		goto again
	}
	return total
}

var saved *int

// A package variable outlives every frame.
func Save() {
	_, p := mk()
	saved = p
}

// Storing through a pointer the caller passed is not storing in the
// region: the caller's variable may outlive the region.
func into(out **int) func() int {
	x := 1
	*out = &x
	return func() int { return x }
}

func main() {
	g, p := pass()
	println(g(), *p)
	println(*h(), *h2(), KeepLast(), KeepField(), Again())
	Save()
	var q *int
	into(&q)
}
