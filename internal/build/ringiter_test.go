package build_test

import (
	"strings"
	"testing"
)

// TestRingIterInlined checks that a loop over a ring's iterator compiles to
// the loop the iterator runs: the iterator and then the loop body are
// inlined through their closures, as for a library container's iterator.
func TestRingIterInlined(t *testing.T) {
	t.Parallel()
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

// TestRingIterErrors checks the uses of a ring's iterator methods the
// compiler rejects. Method values, method expressions and calls through an
// interface are allowed: the iterator's closure is built in the region the
// caller provides (testdata/lib/ringitervalue.p).
func TestRingIterErrors(t *testing.T) {
	t.Parallel()
	const decls = `package main

type Seq interface{ All() func(func(int, int) bool) }

func use(int) {}
`
	cases := []struct{ name, src, want string }{
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := buildError(t, decls+c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}
