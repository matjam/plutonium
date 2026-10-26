package main

// A struct holding a func inside aligned[...] by value: holdsFuncValue
// returns false for every *types2.Wrapper, including volatile, aligned and
// ring, which hold their element by value.
type Getter interface{ Get() int }

type V struct{ f aligned[func() int, 16] }

func (v V) Get() int { return v.f() }

func mk(x int) Getter {
	var v V
	v.f = func() int { return x * 10 }
	return v
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
