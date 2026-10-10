// Defer in the body of a range-over-func loop: the deferred call runs when
// the enclosing function returns. Each defer here is followed by a statement
// that leaves the loop, so it runs at most once per call.

package main

var saved [32]int
var nsaved int

func save(x int) {
	saved[nsaved] = x
	nsaved++
}

func show(name string) {
	print(name, ":")
	for i := 0; i < nsaved; i++ {
		print(" ", saved[i])
	}
	println()
	nsaved = 0
}

func yield4(yield func(int) bool) {
	_ = yield(1) && yield(2) && yield(3) && yield(4)
}

func yield2(yield func(int) bool) {
	_ = yield(1) && yield(2)
}

func count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func deferReturn() int {
	defer save(-1)
	for i := range yield4 {
		if i == 3 {
			defer save(i)
			return i * 10
		}
	}
	defer save(5)
	return 0
}

func deferBreak() {
	defer save(-1)
	for i := range yield4 {
		if i == 2 {
			defer save(i * 10)
			break
		}
	}
	defer save(-2)
}

func deferNotRun() {
	defer save(-1)
	for i := range yield4 {
		if i == 7 {
			defer save(i)
			return
		}
	}
	save(0)
}

func deferNested() {
	defer save(-1)
	for i := range yield2 {
		for j := range count(3) {
			if i == 2 && j == 1 {
				defer save(i*10 + j)
				return
			}
		}
	}
}

func deferBreakOuter() {
	defer save(-1)
outer:
	for i := range yield2 {
		for j := range yield4 {
			if j == 3 {
				defer save(i*100 + j)
				break outer
			}
		}
	}
	save(1)
}

func deferGoto() {
	defer save(-1)
	for i := range yield4 {
		if i == 2 {
			defer save(i)
			goto out
		}
	}
	save(99)
out:
	save(2)
}

func deferClosure() {
	x := 1
	defer save(-1)
	for i := range yield4 {
		if i == 2 {
			x = 7
			defer func() { save(x) }()
			x = 9
			return
		}
	}
}

func deferResults() (r int) {
	defer func() { r *= 2 }()
	for i := range yield4 {
		if i == 3 {
			defer func() { r += 1 }()
			return i
		}
	}
	return 0
}

func deferIfElse(k int) {
	defer save(-1)
	for i := range yield4 {
		if i == 3 {
			defer save(i * k)
			if k > 1 {
				return
			} else {
				break
			}
		}
	}
	save(k)
}

func deferSwitch(k int) {
	defer save(-1)
	for i := range yield4 {
		if i == k {
			defer save(i)
			switch {
			case k > 2:
				return
			default:
				break
			}
			return
		}
	}
	save(0)
}

type counter struct{ n int }

func (c *counter) add(k int) { c.n += k }

func deferMethod() int {
	var c counter
	for i := range yield4 {
		if i == 4 {
			defer c.add(i)
			break
		}
	}
	c.add(1)
	return c.n
}

func main() {
	println(deferReturn())
	show("return")
	deferBreak()
	show("break")
	deferNotRun()
	show("not run")
	deferNested()
	show("nested")
	deferBreakOuter()
	show("break outer")
	deferGoto()
	show("goto")
	deferClosure()
	show("closure")
	println(deferResults())
	deferIfElse(1)
	show("if/else 1")
	deferIfElse(2)
	show("if/else 2")
	deferSwitch(2)
	show("switch 2")
	deferSwitch(3)
	show("switch 3")
	println(deferMethod())
}
