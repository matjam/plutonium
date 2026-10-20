package main

// ring[func() int, 2] holds its elements by value.
type Getter interface{ Get() int }

type V struct{ r ring[func() int, 2] }

func (v V) Get() int {
	f, _ := v.r.Pop()
	return f()
}

func mk(x int) Getter {
	var v V
	v.r.Push(func() int { return x * 10 })
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
