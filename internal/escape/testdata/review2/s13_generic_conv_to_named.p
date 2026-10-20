package main

// a generic function converts its func argument to HF and calls String through Stringer
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

func (h HF) String() string {
	keepF = h
	return "hf"
}

//pu:noinline
func conv[F ~func() int](g F) string { return call(HF(g)) }

//pu:noinline
func f() {
	x := 42
	println(conv(func() int { return x }))
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
