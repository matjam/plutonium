package main

// a named [1]func() int with a String that keeps the element
type Stringer interface{ String() string }

type HF func() int

var keepF func() int

//pu:noinline
func call(s Stringer) string { return s.String() }

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

type A [1]func() int

func (a A) String() string {
	keepF = a[0]
	return "a"
}

//pu:noinline
func f() {
	x := 42
	println(call(A{func() int { return x }}))
}

func main() {
	f()
	println(clobber(1000))
	if keepF == nil {
		println("not kept")
		return
	}
	println(keepF())
}
