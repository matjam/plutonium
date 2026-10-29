// Ported from internal/build/strcount_test.go at the go-cutover tag.

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStringsLeakFree runs the programs that build, copy, store and drop
// counted strings (language spec section 18.8) with the debug allocator and
// its leak report on, at -O0 and -O2: every string they make must be freed
// exactly once, through copies, struct fields, arrays, slices, interface
// boxes, closures, deferred calls, containers, tasks, channels, range
// over functions and every way out of a scope.
func TestStringsLeakFree(t *testing.T) {
	t.Parallel()
	for _, prog := range []string{"run/strcount", "lib/strbuild", "lib/strutf8", "lib/strcontainers", "lib/strconc", "run/fmtquote", "lib/strregions", "lib/regionargs", "run/strreturnloop"} {
		file := filepath.Join("..", "testdata", prog)
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
				t.Parallel()
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
	t.Parallel()
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
	ch := 'a'
	r := rec{name: string(ch) + "b"}
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
	t.Parallel()
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
		for _, checked := range []bool{false, true} {
			gencheck := "--gencheck=false"
			if checked {
				gencheck = "--gencheck"
			}
			exe := buildDebugAlloc(t, src, level, gencheck)
			got, status := runDebugAlloc(t, exe, "")
			want := "debug allocator"
			if checked {
				want = "use of freed memory"
			}
			if status != 2 || !strings.Contains(got, "counted counted") || !strings.Contains(got, want) {
				t.Errorf("-O%d checked=%v: exit status %d, output:\n%s", level, checked, status, got)
			}
		}
	}
}

// TestStringsKernelAlloc checks that kernel code builds strings as it
// allocates with new and make, with no marking (language spec section
// 20.11); that an interrupt handler may only where the allocator does not
// wait is checked in internal/lower (TestInterruptContext).
func TestStringsKernelAlloc(t *testing.T) {
	t.Parallel()
	src := `package main

import (
	"pluto/fmt"
	"pluto/rt"
	"pluto/strings"
)

func label(name string, b []byte) string {
	f := func() string { return "irq " + name }
	return f() + strings.FromUTF8Lossy(b) + fmt.Sprintf("%d", len(b))
}

func setup() string {
	var b [2]byte
	return label("x", b[:])
}

func handler(s string) int {
	return len(strings.SliceFrom(s, 1))
}

func main() {
	_ = handler(setup())
}

//pu:panic
func onPanic(p *rt.Panic) {
	for {
	}
}
`
	if r := tryBuild(t, single(src), "--mode", "kernel", "--gencheck=false", "-c"); r.status != 0 {
		t.Fatalf("build: exit status %d\n%s", r.status, r.out)
	}
}
