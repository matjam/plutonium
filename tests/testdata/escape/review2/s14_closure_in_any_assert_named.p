package main

// an HF in an any, asserted to Stringer in a callee
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
func callAny(a any) string {
	if s, ok := a.(Stringer); ok {
		return s.String()
	}
	return "no"
}

//pu:noinline
func f() {
	x := 42
	var h HF = func() int { return x }
	println(callAny(h))
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
