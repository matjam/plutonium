package main

// Views of a loop's variables used within the iteration behave as in Go
// 1.22, where each iteration has its own variables (spec section 4.5).

func apply(f func() int) int { return f() }

func main() {
	// A closure called in the iteration that made it.
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		println(f(), apply(f))
	}

	// A closure kept, then the loop left at once.
	var kept func() int
	for i, v := range [3]int{5, 6, 7} {
		if v == 6 {
			kept = func() int { return i + v }
			break
		}
	}
	println(kept())

	// A pointer to a body variable, used and dropped each iteration.
	var p *int
	sum := 0
	for i := 0; i < 4; i++ {
		x := i * i
		p = &x
		sum += *p
		p = nil
	}
	println(sum, p == nil)

	// The value passed to the closure as a parameter.
	var fs [3]func(int) int
	for i := range fs {
		fs[i] = func(n int) int { return n * 100 }
	}
	for i, f := range fs {
		println(f(i))
	}

	// A closure using only variables declared outside the loop.
	total := 0
	var add func(int)
	for i := 1; i <= 3; i++ {
		add = func(n int) { total += n }
		add(i)
	}
	add(10)
	println(total)
}
