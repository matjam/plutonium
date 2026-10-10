package build_test

import (
	"strings"
	"testing"
)

// TestRingIterInlined checks that a loop over a ring's iterator compiles to
// the loop the iterator runs: the iterator and then the loop body are
// inlined through their closures, as for a library container's iterator.
func TestRingIterInlined(t *testing.T) {
	const src = `package main

//pu:noinline
func sum(r *ring[int, 8]) int {
	t := 0
	for _, x := range r.All() {
		t += x
	}
	return t
}

//pu:noinline
func total(r *ring[int, 8]) int {
	t := 0
	for x := range r.Values() {
		t += x
	}
	return t
}

func main() {
	var r ring[int, 8]
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}
	println(sum(&r), total(&r))
}
`
	for _, name := range []string{"main.sum", "main.total"} {
		asm := listFunc(t, src, name)
		for _, bad := range []string{"call", "rsp", "panicRangeState"} {
			if strings.Contains(asm, bad) {
				t.Errorf("%s: want a plain loop, found %q\n%s", name, bad, asm)
				break
			}
		}
	}
}

// TestRingIterErrors checks that a ring's iterator methods can only be
// called: like a closure constructor's, their closure is built in the
// caller's frame.
func TestRingIterErrors(t *testing.T) {
	const decls = `package main

type Seq interface{ All() func(func(int, int) bool) }

func use(int) {}
`
	const want = "All returns a closure that captures its parameters"
	cases := []struct{ name, src, want string }{
		{"method value", `
func main() {
	var r ring[int, 4]
	f := r.All
	for i := range f() {
		use(i)
	}
}
`, want},
		{"method expression", `
func main() {
	var r ring[int, 4]
	f := (*ring[int, 4]).All
	for i := range f(&r) {
		use(i)
	}
}
`, want},
		{"deferred", `
func main() {
	var r ring[int, 4]
	defer r.All()
}
`, "not deferred"},
		{"started with go", `
var r ring[int, 4]

func main() {
	go r.All()
}
`, "not started with go"},
		{"interface method", `
func main() {
	var r ring[int, 4]
	var s Seq = &r
	for i := range s.All() {
		use(i)
	}
}
`, "not called through an interface"},
		{"range over a ring that is not addressable", `
func mk() ring[int, 4] {
	var r ring[int, 4]
	return r
}

func main() {
	for i := range mk() {
		use(i)
	}
}
`, "method All of ring[int, 4] has a pointer receiver"},
		{"values as method value", `
func main() {
	var r ring[int, 4]
	f := r.Values
	_ = f
}
`, "Values returns a closure that captures its parameters"},
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
