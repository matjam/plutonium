package main

// An interface with methods can hold a closure when a struct with a func
// field (or an array of funcs) implements it, not only a named func type. The
// closures that mk returns are built in a region of the caller's frame, so
// outer must have its own caller provide the region (spec section 11.5).

type Getter interface{ Get() int }

type V struct{ f func() int }

func (v V) Get() int { return v.f() }

type A [1]func() int

func (a A) Get() int { return a[0]() }

//pu:noinline
func mkV(x int) Getter {
	return V{f: func() int { return x * 10 }}
}

//pu:noinline
func mkA(x int) Getter {
	return A{func() int { return x * 30 }}
}

//pu:noinline
func outerV(x int) Getter { return mkV(x) }

//pu:noinline
func outerA(x int) Getter { return mkA(x) }

type E struct{ f func() string }

func (e E) Error() string { return e.f() }

//pu:noinline
func mkE(s string) error {
	return E{f: func() string { return s }}
}

//pu:noinline
func outerE(s string) error { return mkE(s) }

//pu:noinline
func clobber(n int) int {
	var a [64]int
	for i := range a {
		a[i] = n + i
	}
	s := 0
	for i := range a {
		s += a[i]
	}
	return s
}

func main() {
	v, a := outerV(7), outerA(7)
	err := outerE("expected text")
	println(clobber(1000))
	println(v.Get(), a.Get())
	println(err.Error())
}
