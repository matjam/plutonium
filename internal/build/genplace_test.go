package build_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestGenCheckPlacement checks that the generation checks the optimizer
// moves out of loops, shares between accesses, or keeps across calls that
// cannot free (opt.GenCheck) still catch every use of freed or reset
// memory, and raise no panic for a stale pointer the program does not use.
func TestGenCheckPlacement(t *testing.T) {
	testguard.Parallel(t)
	const prelude = `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"unsafe"
)

type Node struct {
	next *Node
	val  int
}

//pu:noinline
func count(n int) int { return n }

// stale returns a pointer to a freed Node whose slot holds a new one.
func stale() *Node {
	p := alloc.New[Node]()
	alloc.Free(p)
	q := alloc.New[Node]()
	q.val = 7
	return p
}

//pu:noinline
func free(n *Node) { alloc.Free(n) }

//pu:noinline
func reset(a *arena.Chunked) { a.Reset() }

//pu:noinline
func twice(x int) int { return 2 * x }

func use(o owned[Node], a *arena.Chunked) {
	println("a", o.val)
	reset(a)
	println("b", o.val)
}

func main() {
	var _ unsafe.Pointer
`
	const freed = "use of freed memory"
	const wasReset = "use of memory from an arena that was reset at main.reset"
	cases := []struct {
		name, body, want, not string
		status                int
	}{
		{"loop over a stale pointer", `
	p := stale()
	sum, n := 0, count(3)
	for i := 0; i < n; i++ {
		sum += p.val
	}
	println(sum)`, freed, "", 2},
		{"loop that runs no iteration", `
	p := stale()
	sum, n := 0, count(0)
	for i := 0; i < n; i++ {
		sum += p.val
	}
	println("ok", sum)`, "ok 0", "", 0},
		{"nested loops, inner runs none", `
	p := stale()
	sum, n, m := 0, count(3), count(0)
	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			sum += p.val
		}
	}
	println("ok", sum)`, "ok 0", "", 0},
		{"nested loops", `
	p := stale()
	sum, n, m := 0, count(3), count(2)
	for i := 0; i < n; i++ {
		for j := i; j < m; j++ {
			sum += p.val
		}
	}
	println(sum)`, freed, "", 2},
		{"conditional access in a loop", `
	p := stale()
	sum, n, k := 0, count(5), count(9)
	for i := 0; i < n; i++ {
		if i == k {
			sum += p.val
		}
	}
	println("ok", sum)`, "ok 0", "", 0},
		{"loop that frees", `
	p := alloc.New[Node]()
	for i := 0; i < count(3); i++ {
		println("val", p.val)
		if i == 1 {
			free(p)
			_ = alloc.New[Node]()
		}
	}`, freed, "", 2},
		{"new allocation passed to a call that frees it", `
	n := alloc.New[Node]()
	n.val = 1
	free(n)
	_ = alloc.New[Node]()
	println("after", n.val)`, freed, "after", 2},
		{"new allocation across calls that cannot free", `
	n := alloc.New[Node]()
	n.val = twice(1)
	n.val += twice(2)
	println("ok", n.val)`, "ok 6", "", 0},
		{"slab free", `
	slab := alloc.NewSlab(alloc.Pages)
	p := (*Node)(slab.Alloc(16, 8))
	p.val = 1
	slab.Free(unsafe.Pointer(p), 16, 8)
	q := (*Node)(slab.Alloc(16, 8))
	q.val = 2
	println("after", p.val)`, freed, "after", 2},
		{"new allocation in an arena reset by a call", `
	a := arena.NewChunked()
	n := alloc.New[Node](&a)
	n.val = 1
	reset(&a)
	println("after", n.val)`, wasReset, "after", 2},
		{"owner parameter in an arena reset by a call", `
	a := arena.NewChunked()
	o := alloc.Owned[Node](&a)
	o.val = 3
	use(o, &a)`, wasReset, "b ", 2},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runGenCheck(t, prelude+c.body+"\n}\n", opt)
				if status != c.status || !strings.Contains(out, c.want) || c.not != "" && strings.Contains(out, c.not) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}

// listGenFunc returns the listing of function name in src built with
// generation checks.
func listGenFunc(t *testing.T, src, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), OptLevel: 2, Listing: &listing, GenCheck: true}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	text := listing.String()
	i := strings.Index(text, "\n"+name+":\n")
	if i < 0 {
		t.Fatalf("%s not in listing", name)
	}
	text = text[i+1:]
	if j := strings.Index(text, "\n\n"); j >= 0 {
		text = text[:j]
	}
	return text
}

// TestGenCheckCounts checks how many generation checks the optimizer
// leaves in functions whose accesses share them.
func TestGenCheckCounts(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/alloc"

type Node struct {
	left, right *Node
	val         int
}

//pu:noinline
func sum(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

//pu:noinline
func walk(n *Node) int {
	if n.left == nil {
		return n.val
	}
	return walk(n.left) + walk(n.right) + n.val
}

//pu:noinline
func build(d int, a alloc.Allocator) *Node {
	n := alloc.New[Node](a)
	n.val = d
	if d > 0 {
		n.left = build(d-1, a)
		n.right = build(d-1, a)
	}
	return n
}

//pu:noinline
func release(n *Node, a alloc.Allocator) {
	if n.left != nil {
		release(n.left, a)
		release(n.right, a)
	}
	alloc.Free(n, a)
}

//pu:noinline
func fields(n *Node) int {
	if n.left == n.right {
		return n.val
	}
	return 0
}

//pu:noinline
func rows(m [][]int) int {
	t := 0
	for i := 0; i < len(m); i++ {
		r := m[i]
		for j := 0; j < len(r); j++ {
			t += r[j]
		}
	}
	return t
}

func main() {
	a := alloc.Default()
	n := build(3, a)
	m := [][]int{{1, 2}, {3}}
	println(sum([]int{1, 2, 3}), walk(n), rows(m[:]), fields(n))
	release(n, a)
}
`
	for _, c := range []struct {
		fn   string
		want int
		why  string
	}{
		{"main.sum", 1, "each indexed access checks its actual byte extent"},
		{"main.walk", 4, "n.left, n.val on each path, and n.right: each is in another block or after a call"},
		{"main.build", 3, "each new allocation field store comes after a call or in another block"},
		{"main.release", 2, "release frees: n is checked again after the first call"},
		{"main.rows", 2, "a row's pointer and length are adjacent words loaded in one block with nothing between: one check of their 16 bytes; the indexed element has its own"},
		{"main.fields", 2, "n.left and n.right share one check of their 16 bytes; n.val, in another block, has its own"},
	} {
		asm := listGenFunc(t, src, c.fn)
		if n := strings.Count(asm, "call pluto/rt.panicStale"); n != c.want {
			t.Errorf("%s: %d checks, want %d (%s)\n%s", c.fn, n, c.want, c.why, asm)
		}
	}
}

// TestGenCheckDefault checks that a hosted build without a setting has
// generation checks, and that --gencheck=false is the unchecked mode, in
// which a use of freed memory reads whatever the memory holds.
func TestGenCheckDefault(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/alloc"

type Node struct{ val int }

func main() {
	a := alloc.New[Node]()
	b := a
	alloc.Free(a)
	c := alloc.New[Node]() // reuses the slot
	c.val = 9
	println("stale", b.val)
}
`
	// PLUTO_GENCHECK=0 runs the suite in the unchecked mode.
	if os.Getenv("PLUTO_GENCHECK") != "0" {
		if out, status := compileAndRun(t, src, 2); status != 2 || !strings.Contains(out, "use of freed memory") {
			t.Errorf("default: status %d, output %q; want the use of freed memory to panic", status, out)
		}
	}
	if out, status := runUnchecked(t, src, 2); status != 0 || out != "stale 9\n" {
		t.Errorf("--gencheck=false: status %d, output %q; want %q", status, out, "stale 9\n")
	}
}
