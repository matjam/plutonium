package main

// As c20, but the dynamic type is a struct V{fs []func() int} by value and
// the slice refers to an array in the region (captured by g).
type Getter interface{ Get() int }

type V struct{ fs []func() int }

func (v V) Get() int { return v.fs[0]() }

func mk(x int) (func() int, Getter) {
	var arr [1]func() int
	arr[0] = func() int { return x * 10 }
	g := func() int { return len(arr) + x }
	return g, V{fs: arr[:]}
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
