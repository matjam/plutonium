package main

// outer passes mk's Getter on: interfaceMayHoldFunc(Getter) is false, so
// outer may build mk's region in its own frame.
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

func outer(x int) Getter {
	_, it := mk(x)
	return it
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
	it := outer(7)
	println(clobber(1000))
	println(it.Get())
}
