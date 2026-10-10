package main

// f(g()) where g returns several values, into fixed, variadic and builtin
// parameters.

func pair() (int, string) { return 7, "seven" }

func triple() (int, int, int) { return 1, 2, 3 }

func sum(xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func first(a int, rest ...int) int { return a*100 + len(rest) }

func show(n int, s string) { println(n, s) }

func describe(args ...any) int { return len(args) }

func main() {
	show(pair())
	println(sum(triple()))
	println(first(triple()))
	println(describe(pair()))
	println(pair())
	print(triple())
	println()
}
