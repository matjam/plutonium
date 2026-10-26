package main

// A method value (a closure record holding &c) in a struct field.
type Getter interface{ Get() int }

type C struct{ n int }

func (c *C) val() int { return c.n * 10 }

type V struct{ f func() int }

func (v V) Get() int { return v.f() }

func mk(x int) Getter {
	c := C{n: x}
	return V{f: c.val}
}

func outer(x int) Getter { return mk(x) }

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
	g := outer(7)
	println(clobber(1000))
	println(g.Get())
}
