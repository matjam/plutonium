package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// runGenCheck builds src with the generational-reference prototype at the
// given optimisation level and returns its output and exit status.
func runGenCheck(t *testing.T, src string, optLevel int) (string, int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel, GenCheck: true}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	var stdout bytes.Buffer
	cmd := exec.Command(out)
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), 0
}

// TestGenCheck checks that, built with -gencheck, a use of freed memory and
// a double free trap, while correct code runs as before.
func TestGenCheck(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"use after free", `
	a := alloc.New[Node]()
	a.val = 7
	b := a
	alloc.Free(a)
	c := alloc.New[Node]() // reuses the slot
	c.val = 9
	println("stale", b.val)`, "use of freed memory", 2},
		{"interior pointer after free", `
	a := alloc.New[Node]()
	p := &a.val
	alloc.Free(a)
	_ = alloc.New[Node]()
	*p = 1`, "use of freed memory", 2},
		{"double free", `
	a := alloc.New[Node]()
	alloc.Free(a)
	alloc.Free(a)`, "already freed", 2},
		{"correct code", `
	var head *Node
	for i := 0; i < 1000; i++ {
		n := alloc.New[Node]()
		n.val = i
		n.next = head
		head = n
	}
	sum := 0
	for n := head; n != nil; {
		sum += n.val
		next := n.next
		alloc.Free(n)
		n = next
	}
	println("sum", sum)`, "sum 499500", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				testguard.Parallel(t)
				src := "package main\n\nimport \"pluto/alloc\"\n\ntype Node struct {\n\tnext *Node\n\tval  int\n}\n\nfunc main() {" + c.body + "\n}\n"
				out, status := runGenCheck(t, src, opt)
				if status != c.status || !strings.Contains(out, c.want) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, c.want)
				}
			})
		}
	}
}

func TestGenCheckFreestanding(t *testing.T) {
	testguard.Parallel(t)
	cfg := &build.Config{Dir: t.TempDir(), Freestanding: true, GenCheck: true, Stdlib: plutonium.Stdlib()}
	if err := os.WriteFile(filepath.Join(cfg.Dir, "main.p"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := build.Build(cfg)
	if err == nil || !strings.Contains(err.Error(), "hosted builds only") {
		t.Fatalf("error %v, want hosted builds only", err)
	}
}

// TestArenaEpochs checks the arena epochs of a -gencheck build (memory-safety
// design D4): a view, box, string or error in an arena.Chunked that was
// reset, released past it or freed panics with where that happened, also
// when a longer-lived heap container holds it, and correct code runs as
// before.
func TestArenaEpochs(t *testing.T) {
	testguard.Parallel(t)
	const prelude = `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/strconv"
	"pluto/vec"
	"unsafe"
)

type node struct {
	next *node
	val  int
}

func label(a *arena.Chunked, n int) string {
	defer alloc.Use(a).Restore()
	return "item " + strconv.Itoa(n)
}

func box(a *arena.Chunked, n int) shared[node] {
	defer alloc.Use(a).Restore()
	b := alloc.Shared[node]()
	b.val = n
	return b
}

func fail(a *arena.Chunked, n int) error {
	defer alloc.Use(a).Restore()
	return fmt.Errorf("parse failed at %d", n)
}

// heapNames uses the imports a case may leave unused.
func heapNames() vec.Vec[string] {
	_ = unsafe.Pointer(nil)
	return vec.New[string]()
}

func main() {
	a := arena.NewChunked()
`
	const reset = "use of memory from an arena that was reset at main.main (./main.p:"
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"view after reset", `
	p := a.New[node]()
	p.val = 7
	a.Reset() // here
	println(p.val)`, reset, 2},
		{"view after release", `
	m := a.Mark()
	p := a.New[node]()
	p.val = 7
	a.Release(m) // here
	q := a.New[node]() // reuses p's memory
	q.val = 8
	println(p.val)`, reset, 2},
		{"slice in a chunk of its own", `
	m := a.Mark()
	s := a.Slice[int](3000)
	a.Release(m) // here
	_ = a.Slice[int](3000)
	s[2999] = 1`, reset, 2},
		{"string in a heap container", `
	names := vec.New[string](alloc.Default())
	names.Push(label(&a, 4))
	a.Reset() // here
	println(names.At(0))`, reset, 2},
		{"string dropped after reset", `
	names := vec.New[string](alloc.Default())
	names.Push(label(&a, 4))
	a.Reset() // here
	names.Drop()`, reset, 2},
		{"box in a heap container", `
	boxes := vec.New[shared[node]](alloc.Default())
	boxes.Push(box(&a, 5))
	a.Reset() // here
	println(boxes.At(0).val)`, reset, 2},
		{"box dropped after reset", `
	boxes := vec.New[shared[node]](alloc.Default())
	boxes.Push(box(&a, 5))
	a.Reset() // here
	boxes.Drop()`, reset, 2},
		{"error in a heap container", `
	errs := vec.New[error](alloc.Default())
	errs.Push(fail(&a, 3))
	a.Reset() // here
	println(errs.At(0).Error())`, reset, 2},
		{"free of the latest allocation", `
	p := (*node)(a.Alloc(16, 8))
	a.Free(unsafe.Pointer(p), 16, 8) // here
	p.val = 1`, "use of freed memory: an arena allocation freed at main.main (./main.p:", 2},
		{"string dropped after release", `
	for i := 0; i < 2; i++ {
		m := a.Mark()
		s := label(&a, i)
		println(s)
		a.Release(m) // here
	} // s is dropped here, after its memory went back to the arena`, reset, 2},
		{"correct code", `
	keep := a.New[node]()
	keep.val = 1
	for i := 0; i < 1000; i++ {
		m := a.Mark()
		{
			var head *node
			for j := 0; j < 100; j++ {
				n := a.New[node]()
				n.val = j
				n.next = head
				head = n
			}
			s := label(&a, i)
			if len(s) == 0 || head.val != 99 {
				println("wrong")
			}
		} // s is dropped before its memory goes back to the arena
		a.Release(m)
	}
	{
		b := box(&a, 2)
		println("ok", keep.val, b.val, label(&a, 3))
	}
	a.Reset()
	again := a.New[node]()
	again.val = 5
	println("again", again.val)
	a.Drop()`, "ok 1 2 item 3\nagain 5\n", 0},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				testguard.Parallel(t)
				src := prelude + c.body + "\n}\n"
				want := c.want
				if i := strings.Index(src, "// here"); i >= 0 {
					want += strconv.Itoa(strings.Count(src[:i], "\n")+1) + ")"
				}
				out, status := runGenCheck(t, src, opt)
				if status != c.status || !strings.Contains(out, want) {
					t.Fatalf("-O%d: status %d, output %q; want status %d and %q", opt, status, out, c.status, want)
				}
			})
		}
	}
}

// TestSlabLargeAlign checks that the slab allocators honor an alignment
// above a page (arena epoch chunks ask for 64 KiB) and take such memory
// back, with and without generation tags.
func TestSlabLargeAlign(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/alloc"

func main() {
	slab := alloc.NewSlab(alloc.Pages)
	for _, a := range []alloc.Allocator{alloc.Default(), slab} {
		for i := 0; i < 20; i++ {
			p := a.Alloc(9000, 1<<16)
			q := a.Alloc(3000, 8192)
			*(*int)(p) = 1
			*(*int)(q) = 2
			if uintptr(p)%(1<<16) != 0 || uintptr(q)%8192 != 0 {
				println("misaligned")
			}
			a.Free(p, 9000, 1<<16)
			a.Free(q, 3000, 8192)
		}
	}
	println("aligned")
}
`
	for _, opt := range []int{0, 2} {
		if out, status := runGenCheck(t, src, opt); status != 0 || out != "aligned\n" {
			t.Errorf("-gencheck -O%d: status %d, output %q", opt, status, out)
		}
		if out, status := compileAndRun(t, src, opt); status != 0 || out != "aligned\n" {
			t.Errorf("-O%d: status %d, output %q", opt, status, out)
		}
	}
}
