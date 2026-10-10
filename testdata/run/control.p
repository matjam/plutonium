package main

func classify(n int) string {
	switch {
	case n < 0:
		return "negative"
	case n == 0:
		return "zero"
	case n < 10:
		return "small"
	}
	return "large"
}

func weekday(d int) string {
	switch d {
	case 0, 6:
		return "weekend"
	case 1, 2, 3, 4, 5:
		return "weekday"
	default:
		return "invalid"
	}
}

func fall(n int) int {
	r := 0
	switch n {
	case 1:
		r += 1
		fallthrough
	case 2:
		r += 10
		fallthrough
	case 3:
		r += 100
	case 4:
		r += 1000
	}
	return r
}

func labeled() int {
	count := 0
outer:
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			if j == i {
				continue outer
			}
			if i+j > 12 {
				break outer
			}
			count++
		}
	}
	return count
}

func gotoLoop(n int) int {
	i, s := 0, 0
loop:
	if i < n {
		s += i
		i++
		goto loop
	}
	return s
}

func breakInSwitch() int {
	n := 0
	for i := 0; i < 5; i++ {
		switch i {
		case 2:
			break
		default:
			n += i
		}
	}
	return n
}

func collatz(n int) int {
	steps := 0
	for n != 1 {
		if n%2 == 0 {
			n /= 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

func shortCircuit(calls *int, v bool) bool {
	*calls++
	return v
}

func main() {
	for _, n := range []int{-5, 0, 3, 42} {
		println(n, classify(n))
	}
	for d := -1; d < 8; d++ {
		println(d, weekday(d))
	}
	for i := 0; i < 6; i++ {
		println("fall", i, fall(i))
	}
	println("labeled", labeled())
	println("goto", gotoLoop(10))
	println("breakInSwitch", breakInSwitch())
	println("collatz", collatz(27), collatz(97))
	calls := 0
	if shortCircuit(&calls, false) && shortCircuit(&calls, true) {
		println("bad")
	}
	if shortCircuit(&calls, true) || shortCircuit(&calls, false) {
		println("or ok")
	}
	println("calls", calls)
	x := 5
	if y := x * 2; y > 8 {
		println("if-init", y)
	} else if y > 4 {
		println("else-if", y)
	} else {
		println("else", y)
	}
	sum := 0
	for i := range 10 {
		sum += i
	}
	println("range int", sum)
	n := 0
	for {
		n++
		if n == 7 {
			break
		}
	}
	println("infinite", n)
}
