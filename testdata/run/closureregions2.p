// More functions that return closures, built in regions their callers
// provide (language spec section 4.5): large parameters, deferred updates,
// method values, recursion that builds nothing, closures of closures,
// several results, interfaces, defer and go.

package main

type Big struct {
	a, b, c, d, e int
}

// Large param passed by reference, captured.
func Sum(b Big) func() int {
	return func() int { return b.a + b.b + b.c + b.d + b.e }
}

// Named result changed by a deferred closure.
func Deferred() (f func() int) {
	x := 5
	defer func() {
		x = 6
	}()
	f = func() int { return x }
	return
}

// Value receiver method returning closure, used as method value.
type T struct{ n int }

func (t T) Get() func() int { return func() int { return t.n } }

// Recursive function returning error interface: zero-size region cycle.
type E struct{ s string }

func (e *E) Error() string { return e.s }

var errDeep = &E{"deep"}

func walk(n int) error {
	if n == 0 {
		return errDeep
	}
	if err := walk(n - 1); err != nil {
		return err
	}
	return nil
}

// Closure capturing another returned closure.
func Outer() func() int {
	c := Inner(3)
	return func() int { return c() * 2 }
}

func Inner(k int) func() int {
	return func() int { k++; return k }
}

// Multiple results with closures.
func Two(a, b int) (func() int, func() int) {
	return func() int { return a }, func() int { return b }
}

// Interface result holding a method value.
type Getter interface{ Get() func() int }

var gt = T{9}

func AsGetter(n int) Getter { gt.n = n; return &gt }

func Through(g Getter) func() int { return g.Get() }

func main() {
	println(Sum(Big{1, 2, 3, 4, 5})())
	println(Deferred()())
	t := T{4}
	g := t.Get
	println(g()())
	println(walk(3).Error())
	o := Outer()
	println(o(), o())
	x, y := Two(7, 8)
	println(x(), y())
	println(Through(AsGetter(9))())
	defer Inner(1)()
	go Inner(2)
	println("end")
}
