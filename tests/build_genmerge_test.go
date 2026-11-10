// Ported from internal/build/genmerge_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestGenCheckMergeRuntime checks that accesses through one pointer which
// share a generation check (opt.GenCheck: one check covers the fields an
// uninterrupted stretch of a block reads or writes) still panic when the
// memory is freed, reset or reused between them.
//
// The programs that free between two accesses do not import pluto/arena:
// that brings in code after each call (task exit checks) which ends the
// block, and the accesses must be in one block for the test to say
// anything about sharing.
func TestGenCheckMergeRuntime(t *testing.T) {
	t.Parallel()
	const common = `
type Node struct {
	next *Node
	val  int
	more int
}

//pu:noinline
func count(n int) int { return n }

//pu:noinline
func free(n *Node) { alloc.Free(n) }

// fieldsAround and the functions after it access two fields of n in one
// block, on either side of a call that frees.
//
//pu:noinline
func fieldsAround(n *Node, between func(*Node)) int {
	a := n.val
	between(n)
	return a + n.more
}

//pu:noinline
func fieldsAroundFree(n *Node) int {
	a := n.val
	free(n)
	return a + n.more
}

//pu:noinline
func storesAroundFree(n *Node) {
	n.val = 1
	free(n)
	n.more = 2
}

// stale returns a pointer to a freed Node whose slot holds a new one.
func stale() *Node {
	p := alloc.New[Node]()
	alloc.Free(p)
	q := alloc.New[Node]()
	q.val = 7
	return p
}
`
	const plain = `package main

import "pluto/alloc"
` + common + `
func main() {
`
	const withArena = `package main

import (
	"pluto/alloc"
	"pluto/arena"
)
` + common + `
//pu:noinline
func reset(a *arena.Chunked) { a.Reset() }

//pu:noinline
func fieldsAroundReset(n *Node, ar *arena.Chunked) int {
	a := n.val
	reset(ar)
	_ = alloc.New[Node](ar)
	return a + n.more
}

func main() {
`
	const freed = "use of freed memory"
	const wasReset = "use of memory from an arena that was reset at main.reset"
	cases := []struct {
		name, prelude, body, want, not string
		status                         int
	}{
		{"free between two field reads", plain, `
	n := alloc.New[Node]()
	n.val, n.more = 1, 2
	println("after", fieldsAroundFree(n))`, freed, "after", 2},
		{"indirect call that frees between two field reads", plain, `
	n := alloc.New[Node]()
	n.val, n.more = 1, 2
	println("after", fieldsAround(n, free))`, freed, "after", 2},
		{"free between stores to two fields", plain, `
	n := alloc.New[Node]()
	storesAroundFree(n)
	println("after")`, freed, "after", 2},
		{"free between two fields of a new allocation", plain, `
	n := alloc.New[Node]()
	n.val = 1
	a := n.val
	free(n)
	q := alloc.New[Node]()
	q.val = 5
	println("after", a, n.more)`, freed, "after", 2},
		{"free after both fields, then a field again", plain, `
	n := alloc.New[Node]()
	n.val, n.more = 1, 2
	s := n.val + n.more
	free(n)
	_ = alloc.New[Node]()
	println("sum", s)
	println("after", n.val)`, freed, "after", 2},
		{"stale pointer, fields read together", plain, `
	p := stale()
	println("after", p.val+p.more)`, freed, "after", 2},
		{"stale pointer, fields written together", plain, `
	p := stale()
	p.val, p.more = 1, 2
	println("after")`, freed, "after", 2},
		{"free between two fields in a loop", plain, `
	p := alloc.New[Node]()
	p.more = 9
	for i := 0; i < count(3); i++ {
		v := p.val
		if i == 1 {
			free(p)
			_ = alloc.New[Node]()
		}
		println("iteration", i, v, p.more)
	}`, freed, "iteration 1", 2},
		{"fields of a live node", plain, `
	n := alloc.New[Node]()
	n.val, n.more = 3, 4
	s := n.val + n.more
	free(alloc.New[Node]())
	s += n.val + n.more
	println("ok", s)`, "ok 14", "", 0},
		{"arena reset between two field reads", withArena, `
	a := arena.NewChunkedUnsafe()
	n := alloc.New[Node](&a)
	n.val, n.more = 1, 2
	println("after", fieldsAroundReset(n, &a))`, wasReset, "after", 2},
		{"arena reset between two fields of a new allocation", withArena, `
	a := arena.NewChunkedUnsafe()
	n := alloc.New[Node](&a)
	n.val = 1
	x := n.val
	reset(&a)
	_ = alloc.New[Node](&a)
	println("after", x, n.more)`, wasReset, "after", 2},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				out, status := runGenCheck(t, c.prelude+c.body+"\n}\n", opt)
				if status != c.status || !strings.Contains(out, c.want) || c.not != "" && strings.Contains(out, c.not) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}
