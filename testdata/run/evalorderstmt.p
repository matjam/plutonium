package main

// Order of evaluation in assignments, statements and operators (language
// spec section 11.26): the calls of a statement come first, left to right,
// and the other operands, the left side's indexes and indirections among
// them, are read after them.

type P struct{ x int }

func (p *P) Add(n int) int { return p.x + n }

var g int

//pu:noinline
func pair(i *int) (int, bool) {
	*i = 2
	return 7, true
}

func main() {
	// A tuple assignment: the index on the left is read after the call.
	s := []int{0, 0, 0}
	i := 0
	var b bool
	s[i], b = pair(&i)
	println(s[0], s[1], s[2], b)

	// A map key on the left.
	m := map[int]int{}
	k := 1
	m[k] = func() int { k = 2; return 5 }()
	println(len(m), m[1], m[2])

	// A map compound assignment.
	m2 := map[int]int{1: 1, 2: 2}
	k = 1
	m2[k] += func() int { k = 2; return 10 }()
	println(m2[1], m2[2])

	// A pointer on the left.
	v1, v2 := 0, 0
	p := &v1
	*p = func() int { p = &v2; return 3 }()
	println(v1, v2)

	// A field through a pointer on the left.
	q1, q2 := &P{}, &P{}
	q := q1
	q.x = func() int { q = q2; return 4 }()
	println(q1.x, q2.x)

	// A swap.
	a := []int{1, 2, 3}
	x, y := 0, 2
	a[x], a[y] = a[y], a[x]
	println(a[0], a[1], a[2])

	// Operators.
	c := 1
	bump := func() int { c++; return c }
	println(c == bump(), c<<uint(bump()), -c+bump(), ^c+bump())
	c = 1
	println(float64(c) + float64(bump()))
	str := "ab"
	si := 0
	println(str[si] + byte(func() int { si = 1; return 0 }()))
	arr := [2]int{1, 2}
	println(arr == func() [2]int { arr = [2]int{3, 4}; return [2]int{3, 4} }())

	// A pointer receiver read after the calls in the arguments.
	pr := &P{1}
	p100 := &P{100}
	println(pr.Add(func() int { pr = p100; return 0 }()))

	// Switch tags and conditions.
	t := 1
	switch t + func() int { t = 10; return 0 }() {
	case 10:
		println("tag 10")
	default:
		println("tag other")
	}
	t = 1
	if t < func() int { t = 10; return 5 }() {
		println("lt")
	} else {
		println("ge")
	}

	// Sends.
	chs := []chan int{make(chan int, 1), make(chan int, 1)}
	ci := 0
	chs[ci] <- func() int { ci = 1; return 8 }()
	println(len(chs[0]), len(chs[1]))
	cv := 1
	ch := make(chan int, 1)
	ch <- cv + func() int { cv = 20; return 0 }()
	println(<-ch)

	// A receive on the right of an indexed assignment.
	ri := 0
	rs := []int{0, 0}
	ch <- 9
	rs[ri] = <-ch
	println(rs[0], rs[1])

	// Variable declarations.
	dv := 1
	var d1, d2 = dv, func() int { dv = 3; return dv }()
	println(d1, d2)

	// Strings.
	ss := "a"
	sf := func() string { ss = "b"; return "c" }
	println(ss + sf() + ss)

	// Deferred calls take their arguments when the defer runs.
	func() {
		dd := 1
		defer println("deferred", dd, func() int { dd = 2; return dd }())
	}()

	// A function value.
	fa := func(n int) int { return n + 1 }
	fb := func(n int) int { return n + 100 }
	fv := fa
	println(fv(func() int { fv = fb; return 0 }()))
}
