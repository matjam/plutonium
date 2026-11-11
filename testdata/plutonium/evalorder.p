package main

// Order of evaluation (language spec section 11.26) where Plutonium and Go's
// gc differ: the receiver of an interface method call and the operand of a
// type assertion are read after the calls of the expression, and the default
// expressions of omitted parameters are evaluated after the explicit
// arguments.

type I interface{ M(int) int }

type A struct{ n int }

func (a A) M(k int) int { return a.n + k }

var gv = 1

var fifty I = A{50}

func bumpg() int {
	gv = 5
	return gv
}

func dflt(a int, b int = bumpg()) int { return a*100 + b }

func main() {
	// An interface receiver.
	var iv I = A{1}
	println(iv.M(func() int { iv = fifty; return 0 }()))

	// A type assertion.
	var e any = 1
	println(e.(int) + func() int { e = 10; return 0 }())

	// The explicit argument is read before the default's call.
	println(dflt(gv))
	gv = 1
	println(dflt(gv + func() int { gv = 2; return 0 }()))
}
