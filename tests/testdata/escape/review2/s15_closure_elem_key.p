package main

// element shape of a closure array in a callee: g passes HF(p[1]) as a Stringer
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
func g(p *[2]HF) string { return call(p[1]) }

//pu:noinline
func f() {
	x := 42
	var arr [2]HF
	arr[1] = func() int { return x }
	println(g(&arr))
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
