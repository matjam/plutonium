package main

// mk returns a closure g (so it has a region) and a Getter whose dynamic type
// is *V. v is captured by g, so it lives in the region; v.f holds another
// closure. Getter is implemented only by *V, which holds a func only through
// a pointer, so interfaceMayHoldFunc(Getter) is false and v.f's closure may
// be left in mk's frame.
type Getter interface{ Get() int }

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
