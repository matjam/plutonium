package main

// The dynamic type is a named slice of funcs with a method.
type Getter interface{ Get() int }

type FS []func() int

func (s FS) Get() int { return s[0]() }

func mk(x int) (func() int, Getter) {
	var arr [1]func() int
	arr[0] = func() int { return x * 10 }
	g := func() int { return len(arr) + x }
	return g, FS(arr[:])
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
