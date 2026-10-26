package main

// a pointer-shaped struct of one func field in an interface; its String keeps the func
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

type D struct{ g func() int }

func (d D) String() string {
	keepF = d.g
	return "d"
}

//pu:noinline
func f() {
	x := 42
	println(call(D{func() int { return x }}))
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
