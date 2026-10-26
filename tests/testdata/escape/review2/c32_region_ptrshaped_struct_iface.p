package main

// A pointer-shaped struct (one pointer field) in Getter, pointing at an array
// that the returned closure g captures (so it is in the region), holding a
// second closure. Getter is implemented only by P, which holds a func only
// through a pointer.
type Getter interface{ Get() int }

type P struct{ p *[1]func() int }

func (q P) Get() int { return q.p[0]() }

func mk(x int) (func() int, Getter) {
	var arr [1]func() int
	arr[0] = func() int { return x * 10 }
	g := func() int { return len(arr) + x }
	return g, P{p: &arr}
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
