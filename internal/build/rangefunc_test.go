package build_test

import (
	"strings"
	"testing"
)

// TestRangeFuncErrors checks the range-over-func loops and closure
// constructors the compiler rejects: a defer that could run once per
// iteration, a deferred literal that keeps the loop body's variables past
// the body, and a closure constructor used other than by calling it.
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
		{"constructor as func value", `
func main() {
	f := count
	for x := range f(3) {
		use(x)
	}
}
`, "count returns a closure that captures its parameters"},
		{"constructor as method value", `
func main() {
	c := &Counter{n: 2}
	f := c.All
	for x := range f() {
		use(x)
	}
}
`, "All returns a closure that captures its parameters"},
		{"constructor as interface method", `
func main() {
	var s Seq = &Counter{n: 2}
	for x := range s.All() {
		use(x)
	}
}
`, "All returns a closure that captures its parameters"},
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
