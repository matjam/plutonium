package main

// c20 with a named func type that implements Getter, so that
// interfaceMayHoldFunc(Getter) is true: shows whether &v of a variable that
// the returned closure captures may be returned at all.
type Getter interface{ Get() int }

type HF func() int

func (h HF) Get() int { return h() }

type V struct {
	f func() int
	n int
}

func (v *V) Get() int { return v.f() }

func mk(x int) (func() int, Getter) {
	var v V
	v.n = x
	v.f = func() int { return v.n * 10 }
	g := func() int { return v.n + 1 }
	return g, &v
}

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
	g, it := mk(7)
	println(clobber(1000))
	println(g(), it.Get())
}
