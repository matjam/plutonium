package build_test

import (
	"strings"
	"testing"
)

// TestRangeFuncInlined checks that a loop over a simple iterator compiles
// to the loop the iterator runs: the iterator, then the loop body, are
// inlined through their closures, the captured variables live in
// registers, and the iteration state checks fold away.
func TestRangeFuncInlined(t *testing.T) {
	const src = `package main

func All(s []int) func(func(int, int) bool) {
	return func(yield func(int, int) bool) {
		for i, x := range s {
			if !yield(i, x) {
				return
			}
		}
	}
}

//pu:noinline
func sum(s []int) int {
	t := 0
	for _, x := range All(s) {
		t += x
	}
	return t
}

//pu:noinline
func find(s []int, k int) int {
	for i, x := range All(s) {
		if x == k {
			return i
		}
	}
	return -1
}

//pu:noinline
func pairs(s []int) int {
	n := 0
	for i, x := range All(s) {
		for j, y := range All(s) {
			if j >= i {
				break
			}
			if x+y == 7 {
				n++
			}
		}
	}
	return n
}

func main() {
	s := []int{1, 2, 3, 4, 5, 6}
	println(sum(s), find(s, 4), pairs(s))
}
`
	for _, name := range []string{"main.sum", "main.find", "main.pairs"} {
		asm := listFunc(t, src, name)
		for _, bad := range []string{"call", "rsp", "panicRangeState"} {
			if strings.Contains(asm, bad) {
				t.Errorf("%s: want a plain loop, found %q\n%s", name, bad, asm)
				break
			}
		}
	}
}

// TestRangeFuncErrors checks the range-over-func loops the compiler
// rejects: a defer that could run once per iteration, and a deferred
// literal that keeps the loop body's variables past the body.
func TestRangeFuncErrors(t *testing.T) {
	const decls = `package main

func seq(yield func(int) bool) {
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

type Counter struct{ n int }

func (c *Counter) All() func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < c.n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

type Seq interface{ All() func(func(int) bool) }

func use(int) {}
`
	cases := []struct{ name, src, want string }{
		{"defer each iteration", `
func main() {
	for x := range seq {
		defer use(x)
	}
}
`, "defer in the body of a range-over-func loop must be followed by"},
		{"defer then continue", `
func main() {
	for x := range seq {
		if x > 0 {
			defer use(x)
			continue
		}
	}
}
`, "defer in the body of a range-over-func loop must be followed by"},
		{"defer leaves inner loop only", `
func main() {
	for x := range seq {
		for y := range seq {
			defer use(x + y)
			break
		}
	}
}
`, "defer in the body of a range-over-func loop must be followed by"},
		{"defer one branch stays", `
func main() {
	for x := range seq {
		defer use(x)
		if x > 1 {
			return
		}
	}
}
`, "defer in the body of a range-over-func loop must be followed by"},
		{"defer then continue from a loop", `
func main() {
outer:
	for x := range seq {
		defer use(x)
		for i := 0; i < 2; i++ {
			if i == x {
				continue outer
			}
		}
		return
	}
}
`, "defer in the body of a range-over-func loop must be followed by"},
		{"defer then goto in the body", `
func main() {
	for x := range seq {
	again:
		if x > 5 {
			defer use(x)
			goto again
		}
		return
	}
}
`, "defer inside a loop is not supported"}, // the goto makes a loop in the body
		{"defer in a loop in the body", `
func main() {
	for x := range seq {
		for i := 0; i < x; i++ {
			defer use(i)
		}
		return
	}
}
`, "defer inside a loop is not supported"},
		{"deferred literal captures body variable", `
func main() {
	for x := range seq {
		defer func() { use(x) }()
		return
	}
}
`, "cannot capture x, a variable of the loop body"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := buildError(t, decls+c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}
