package main

// a method value h.String of a closure converted to HF, passed to a keeper of func values
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

var keepS func() string

//pu:noinline
func callf(g func() string) string { return g() }

//pu:noinline
func f() {
	x := 42
	h := HF(func() int { return x })
	println(callf(h.String))
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
