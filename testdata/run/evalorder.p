package main

// Order of evaluation (language spec section 11.26): the calls, method
// calls, receives and logical operations of an expression are evaluated
// first, left to right; the other operands are read after them.

type S struct{ x, y int }

type C struct{ n int }

func (c C) Get() int { return c.n }

func (c *C) Bump() int {
	c.n += 10
	return c.n
}

var g int

//pu:noinline
func incg() int {
	g++
	return g
}

var trace [8]int
var ntrace int

//pu:noinline
func note(n int) int {
	trace[ntrace] = n
	ntrace++
	return n
}

func printTrace() {
	for _, n := range trace[:ntrace] {
		print(n, " ")
	}
	println()
	ntrace = 0
}

//pu:noinline
func pair() (int, int) {
	g += 100
	return g, 1
}

//pu:noinline
func add(a, b int) int { return a + b }

func main() {
	a := 1
	f := func() int { a++; return a }

	// Composite literal elements.
	x := []int{a, f()}
	println(x[0], x[1])

	// Binary operators.
	n := a + f()
	println(n)

	// Array and struct literals.
	ar := [3]int{a, f(), a}
	println(ar[0], ar[1], ar[2])
	st := S{1, 2}
	h := func() int { st.x = 100; return 7 }
	v := S{st.x, h()}
	println(v.x, v.y)
	w := S{y: h(), x: st.y}
	println(w.x, w.y)

	// Package variables.
	g = 0
	println(g + incg() + g)
	println(g*10 + incg())

	// The index on the left of an assignment is read after the call on
	// the right.
	s := []int{10, 20, 30}
	i := 0
	nf := func() int { i++; return 5 }
	s[i], i = nf(), 0
	println(s[0], s[1], s[2], i)
	arr := [3]int{}
	j := 0
	arr[j] = func() int { j = 2; return 9 }()
	println(arr[0], arr[1], arr[2])

	// Compound assignment.
	k := 1
	ks := []int{0, 0, 0}
	ks[k] += func() int { k = 2; return 4 }()
	println(ks[0], ks[1], ks[2])

	// Strings.
	str := "ab"
	cs := func() string { str = "xyz"; return "!" }
	println(str + cs())

	// Return statements.
	ret := func() (int, int) {
		r := 1
		return r, func() int { r = 5; return 0 }()
	}
	r1, r2 := ret()
	println(r1, r2)

	// Arguments: non-call arguments are read after the calls among them.
	g = 0
	println(add(g, incg()), add(incg(), g))

	// A call happens after the calls in its own arguments, and calls run
	// left to right.
	note(1)
	_ = add(note(2), add(note(3), note(4))) + note(5)
	printTrace()

	// Method calls: a value receiver is read after the arguments' calls.
	c := C{1}
	println(c.Get() + c.Bump())
	c2 := C{1}
	println(c2.n + c2.Bump())

	// Indexing and slicing operands.
	idx := 0
	sl := []int{1, 2, 3, 4}
	println(sl[idx] + func() int { idx = 3; return 0 }())
	idx = 0
	t := sl[idx:func() int { idx = 1; return 3 }()]
	println(len(t), t[0])

	// Pointer indirections.
	pv := 1
	p := &pv
	println(*p + func() int { pv = 50; return 0 }())

	// Logical operations are evaluated with the calls; their operands in
	// order, the right one only when needed.
	b := false
	bs := []bool{b, b || func() bool { b = true; return false }()}
	println(bs[0], bs[1])

	// A call with several results as the only argument.
	g = 0
	println(g, add(pair()))

	// Receives come first too.
	ch := make(chan int, 1)
	rv := 1
	ch <- 5
	println(rv + func() int { rv = 20; return <-ch }())
	ch <- 6
	rs := []int{rv, <-ch}
	println(rs[0], rs[1])

	// Map literals and map index expressions.
	ma := 1
	mf := func() int { ma++; return ma }
	m := map[int]int{ma: mf()}
	println(len(m), m[2])
	mm := map[int]int{1: 10}
	println(mm[1] + func() int { mm[1] = 50; return 0 }())
}
