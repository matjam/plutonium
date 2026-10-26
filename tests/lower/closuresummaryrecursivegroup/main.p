package main

func f0(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f1(f2(g, x-1), x-2)
}

func f1(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f2(f3(g, x-1), x-2)
}

func f2(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f3(f4(g, x-1), x-2)
}

func f3(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f4(f5(g, x-1), x-2)
}

func f4(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f5(f6(g, x-1), x-2)
}

func f5(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f6(f7(g, x-1), x-2)
}

func f6(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f7(f8(g, x-1), x-2)
}

func f7(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f8(f9(g, x-1), x-2)
}

func f8(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f9(f10(g, x-1), x-2)
}

func f9(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f10(f11(g, x-1), x-2)
}

func f10(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f11(f12(g, x-1), x-2)
}

func f11(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f12(f13(g, x-1), x-2)
}

func f12(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f13(f14(g, x-1), x-2)
}

func f13(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f14(f15(g, x-1), x-2)
}

func f14(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f15(f16(g, x-1), x-2)
}

func f15(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f16(f17(g, x-1), x-2)
}

func f16(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f17(f18(g, x-1), x-2)
}

func f17(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f18(f19(g, x-1), x-2)
}

func f18(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f19(f20(g, x-1), x-2)
}

func f19(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f20(f21(g, x-1), x-2)
}

func f20(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f21(f22(g, x-1), x-2)
}

func f21(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f22(f23(g, x-1), x-2)
}

func f22(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f23(f0(g, x-1), x-2)
}

func f23(g func() int, x int) func() int {
	if x == 0 {
		return g
	}
	return f0(f1(g, x-1), x-2)
}

func main() {
	println(f0(func() int { return 1 }, 3)())
}
