package main

// No other closure is returned: mk returns &v as a Getter, v.f a closure
// capturing v. With no region, v is in mk's frame: E3001 expected.
type Getter interface{ Get() int }

type V struct {
	f func() int
	n int
}

func (v *V) Get() int { return v.f() }

func mk(x int) Getter {
	var v V
	v.n = x
	v.f = func() int { return v.n * 10 }
	return &v
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
	it := mk(7)
	println(clobber(1000))
	println(it.Get())
}
