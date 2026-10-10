package main

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func divmod(a, b int) (int, int) { return a / b, a % b }

func named(x int) (sq, cube int) {
	sq = x * x
	cube = sq * x
	return
}

func sum(xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func apply(f func(int) int, x int) int { return f(x) }

func double(x int) int { return x * 2 }

func many(a, b, c, d, e, f, g, h, i int, x, y float64) int {
	return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i + int(x*10) + int(y*100)
}

func deferOrder() (r int) {
	defer func() { r *= 2 }()
	defer func() { r += 3 }()
	r = 5
	return r
}

func deferArgs() {
	x := 1
	defer println("deferred x", x)
	x = 2
	println("x now", x)
}

func counter() int {
	n := 0
	inc := func() { n++ }
	for i := 0; i < 5; i++ {
		inc()
	}
	add := func(k int) int { return n + k }
	return add(100)
}

func swap(a, b int) (int, int) { return b, a }

func main() {
	println("fib", fib(20))
	q, r := divmod(47, 5)
	println("divmod", q, r)
	s, c := named(7)
	println("named", s, c)
	println("sum", sum(), sum(1), sum(1, 2, 3, 4))
	nums := []int{10, 20, 30}
	println("sum slice", sum(nums...))
	println("apply", apply(double, 21))
	triple := func(x int) int { return 3 * x }
	println("apply lit", apply(triple, 7))
	println("many", many(1, 2, 3, 4, 5, 6, 7, 8, 9, 1.5, 2.25))
	println("deferOrder", deferOrder())
	deferArgs()
	println("counter", counter())
	a, b := swap(1, 2)
	a, b = b, a
	println("swap", a, b)
	fs := [3]func(int) int{double, triple, func(x int) int { return x - 1 }}
	t := 0
	for i := range fs {
		t += fs[i](10)
	}
	println("func array", t)
}
