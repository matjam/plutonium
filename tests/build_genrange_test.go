// Ported from internal/build/genrange_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestGenCheckRangeRuntime checks the range checks the optimizer puts
// before counted loops (opt.GenCheck, hoistRange): a loop over freed
// memory panics, one that runs no iteration does not, a free made between
// iterations by a call is caught at the next access, and a loop over all
// of a live allocation, or part of one, runs.
func TestGenCheckRangeRuntime(t *testing.T) {
	t.Parallel()
	const prelude = `package main

import "pluto/alloc"

type Block [64]int

//pu:noinline
func count(n int) int { return n }

//pu:noinline
func free(p *Block) { alloc.Free(p) }

//pu:noinline
func sum(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

//pu:noinline
func fill(s []int) {
	for i := 0; i < len(s); i++ {
		s[i] = i
	}
}

// stale returns a slice of a freed Block whose memory holds a new one.
func stale() []int {
	p := alloc.New[Block]()
	s := p[:]
	free(p)
	q := alloc.New[Block]()
	q[5] = 7
	return s
}

// sumFreeing sums s, freeing p and handing its memory out again after
// element 3.
//
//pu:noinline
func sumFreeing(s []int, p *Block) int {
	t := 0
	for i := 0; i < len(s); i++ {
		println("iteration", i)
		t += s[i]
		if i == 3 {
			free(p)
			_ = alloc.New[Block]()
		}
	}
	return t
}

func main() {
`
	const freed = "use of freed memory"
	cases := []struct {
		name, body, want, not string
		status                int
	}{
		{"loop over a freed slice", `
	s := stale()
	println("after", sum(s))`, freed, "after", 2},
		{"loop writing a freed slice", `
	s := stale()
	fill(s)
	println("after")`, freed, "after", 2},
		{"loop over a freed slice that runs no iteration", `
	s := stale()
	println("ok", sum(s[:count(0)]))`, "ok 0", "", 0},
		{"free between iterations by a call", `
	p := alloc.New[Block]()
	fill(p[:])
	println("after", sumFreeing(p[:], p))`, freed, "iteration 5", 2},
		{"loop over a whole live allocation", `
	p := alloc.New[[20000]int]()
	fill(p[:])
	n := sum(p[:])
	alloc.Free(p)
	println("ok", n)`, "ok 199990000", "", 0},
		{"loop over part of a live allocation", `
	p := alloc.New[Block]()
	fill(p[:])
	n := sum(p[3:count(10)])
	free(p)
	println("ok", n)`, "ok 42", "", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				out, status := runGenCheck(t, prelude+c.body+"\n}\n", opt)
				if status != c.status || !strings.Contains(out, c.want) || c.not != "" && strings.Contains(out, c.not) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}
