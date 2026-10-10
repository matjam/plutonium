package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// TestStringsLeakFree runs the programs that build, copy, store and drop
// counted strings (language spec section 6.10) with the debug allocator and
// its leak report on, at -O0 and -O2: every string they make must be freed
// exactly once, through copies, struct fields, arrays, slices, interface
// boxes, closures, deferred calls, containers, puroutines, channels, range
// over functions and every way out of a scope.
func TestStringsLeakFree(t *testing.T) {
	for _, prog := range []string{"run/strcount", "lib/strbuild", "lib/strutf8", "lib/strcontainers", "lib/strconc", "run/fmtquote"} {
		file := filepath.Join("..", "..", "testdata", prog)
		src, err := os.ReadFile(file + ".p")
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(file + ".out")
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []int{0, 2} {
			t.Run(prog+"/O"+string(rune('0'+level)), func(t *testing.T) {
				exe := buildDebugAlloc(t, string(src), level)
				got, status := runDebugAlloc(t, exe, "")
				if status != 0 || got != string(want) {
					t.Errorf("exit status %d, output:\n%s\nwant:\n%s", status, got, want)
				}
			})
		}
	}
}

// TestStringHoldElision checks that a string borrowed from memory a call
// could change is passed without counting when no call it is held across
// can release a string (opt.ElideStringHolds), and is counted when one
// can.
func TestStringHoldElision(t *testing.T) {
	const src = `package main

type rec struct{ name string }

//pu:noinline
func length(s string) int { return len(s) }

//pu:noinline
func replace(r *rec, s string) int {
	r.name = "other"
	return len(s)
}

//pu:noinline
func pure(p *rec) int { return length(p.name) }

//pu:noinline
func risky(p *rec) int { return replace(p, p.name) }

func main() {
	r := rec{name: string([]byte{'a', 'b'})}
	println(pure(&r), risky(&r), r.name)
}
`
	if pure := listFunc(t, src, "main.pure"); strings.Contains(pure, "strFree") || strings.Contains(pure, "strRelease") {
		t.Errorf("main.pure counts the string it passes:\n%s", pure)
	}
	if risky := listFunc(t, src, "main.risky"); !strings.Contains(risky, "strFree") {
		t.Errorf("main.risky does not keep the string it passes alive:\n%s", risky)
	}
	exe := buildDebugAlloc(t, src, 2)
	if got, status := runDebugAlloc(t, exe, ""); status != 0 || got != "2 2 other\n" {
		t.Errorf("exit status %d, output %q", status, got)
	}
}

// TestStringDoubleRelease checks that a string released once too often,
// here a copy made through unsafe that took no reference, stops the
// program instead of freeing memory twice.
func TestStringDoubleRelease(t *testing.T) {
	src := `package main

import (
	"pluto/strings"
	"unsafe"
)

func main() {
	s := strings.Clone("counted")
	var t string
	*(*[3]uintptr)(unsafe.Pointer(&t)) = *(*[3]uintptr)(unsafe.Pointer(&s))
	println(s, t)
}
`
	for _, level := range []int{0, 2} {
		exe := buildDebugAlloc(t, src, level)
		got, status := runDebugAlloc(t, exe, "")
		if status != 2 || !strings.Contains(got, "counted counted") || !strings.Contains(got, "debug allocator") {
			t.Errorf("-O%d: exit status %d, output:\n%s", level, status, got)
		}
	}
}

// TestStringsKernelAlloc checks that kernel code builds strings in
// functions marked //pu:alloc, and only there (language spec section 12.7):
// a program that does so compiles, and the errors for one that does not are
// in cmd/pluto/testdata/diag/kernel_strings.
func TestStringsKernelAlloc(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "pluto/rt"

//pu:alloc
func label(name string, b []byte) string {
	f := func() string { return "irq " + name } // inherits //pu:alloc
	return f() + string(b)
}

//pu:alloc
func setup() string {
	var b [2]byte
	return label("x", b[:])
}

func handler(s string) int {
	return len(s[1:]) // slicing and len never allocate
}

//pu:alloc
func main() {
	_ = handler(setup())
}

//pu:panic
func onPanic(p *rt.Panic) {
	for {
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "k.o"), Stdlib: plutonium.Stdlib(), Kernel: true, CompileOnly: true}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
}
