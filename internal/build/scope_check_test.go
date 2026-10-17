package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestScopeChecks covers what lowering checks of a scoped task: a loop that
// steps by a variable gets a run-time check that the step is positive, and
// a task may not use a variable whose count is not atomic.
func TestScopeChecks(t *testing.T) {
	testguard.Parallel(t)
	const step = `package main

import "pluto/sync"

func fill(out []int, k int) {
	var s sync.Scope
	for i := 0; i < len(out); i += k {
		s.Task(func() {
			w := out[i:min(i+k, len(out))]
			for j := range w {
				w[j] = i
			}
		})
	}
}

func main() {
	var out [4]int
	fill(out[:], 2)
	println(out[0], out[1], out[2], out[3])
	fill(out[:], 0)
	println("not reached")
}
`
	for _, level := range []int{0, 2} {
		got, status := compileAndRun(t, step, level)
		const want = "0 0 2 2\npanic: runtime error: the step of a loop that starts tasks of a sync.Scope is not positive: 0\n"
		if status != 2 || !strings.HasPrefix(got, want) {
			t.Errorf("O%d: exit status %d, output:\n%s\nwant status 2 and:\n%s", level, status, got, want)
		}
	}
	const overflow = `package main

import "pluto/sync"

func rows(out []int, k, from, to int) {
	var s sync.Scope
	for i := from; i < to; i++ {
		s.Task(func() {
			if k > 1 {
				w := out[i*k : (i+1)*k]
				w[0] = i
			}
		})
	}
}

func main() {
	var out [8]int
	rows(out[:], 2, 0, 4)
	println(out[0], out[6])
	rows(out[:], 1<<62, 2, 3)
	println("not reached")
}
`
	for _, level := range []int{0, 2} {
		got, status := compileAndRun(t, overflow, level)
		const want = "0 3\npanic: runtime error: integer overflow in an index of a task of a sync.Scope\n"
		if status != 2 || !strings.HasPrefix(got, want) {
			t.Errorf("O%d: exit status %d, output:\n%s\nwant status 2 and:\n%s", level, status, got, want)
		}
	}
	const shared = `package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Stats struct{ n int }

func main() {
	st := alloc.Shared[Stats]()
	var s sync.Scope
	s.Task(func() { println(st.n) })
}
`
	err := buildError(t, shared)
	if err == nil || !strings.Contains(err.Error(), "uses st, of type shared[main.Stats], which is not sendable") {
		t.Errorf("got %v, want the task's use of a shared[T] rejected", err)
	}
}

// TestScopeOverlapCheck covers the check before each task of an elementwise
// loop over two slices (language spec section 19.4, Memory reached through
// views): the slices must start at the same element or not overlap. The
// check is made in every build, unchecked ones included.
func TestScopeOverlapCheck(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/sync"

func double(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * 2 })
	}
}

func main() {
	buf := [8]int{1, 2, 3, 4, 5, 6, 7, 8}
	var out [8]int

	// Different arrays, the same slice, the same start with other lengths.
	double(buf[:4], out[:4])
	println(out[0], out[1], out[2], out[3])
	double(buf[:], buf[:])
	println(buf[0], buf[1], buf[7])
	double(buf[:2], buf[:])
	println(buf[0], buf[1], buf[2])

	// Halves of one array do not overlap; empty slices overlap nothing.
	double(out[4:], out[:4])
	println(out[0], out[1])
	double(nil, buf[:])
	double(buf[3:3], buf[:])
	println("before")

	// The same elements at different offsets.
	double(buf[1:5], buf[:4])
	println("not reached")
}
`
	const want = "2 4 6 8\n2 4 16\n4 8 6\n0 0\n"
	for _, level := range []int{0, 2} {
		for name, run := range map[string]func(*testing.T, string, int) (string, int){
			"checked":   compileAndRun,
			"unchecked": runUnchecked,
		} {
			got, status := run(t, src, level)
			const tail = "before\npanic: runtime error: the slices in and out, used by the tasks of a sync.Scope, overlap without starting at the same element\n"
			if status != 2 || !strings.HasPrefix(got, want) || !strings.Contains(got, tail) {
				t.Errorf("O%d %s: exit status %d, output:\n%s\nwant status 2, starting with:\n%s\nand containing:\n%s", level, name, status, got, want, tail)
			}
		}
	}
}
