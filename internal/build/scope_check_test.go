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
