package build_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// runDebugAllocArgs runs prog with arguments, as runDebugAlloc runs it.
func runDebugAllocArgs(t *testing.T, prog string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(prog, args...)
	cmd.Env = append(os.Environ(), "PLUTO_DEBUGALLOC=")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// TestMapsLeakFree runs the programs that make, copy, store, range over
// and drop built-in maps (language spec section 4.12) with the debug
// allocator and its leak report on, at -O0 and -O2: every table, box and
// string they make must be freed exactly once, through copies, struct
// fields, arrays, maps of maps, deletion, clear, rebuilds during range
// loops, explicit allocators and arenas.
func TestMapsLeakFree(t *testing.T) {
	testguard.Parallel(t)
	for _, prog := range []string{"run/maps", "run/maprange", "run/mapkeys", "run/mapgeneric", "run/mapfmt", "lib/mapalloc", "lib/hash"} {
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
				testguard.Parallel(t)
				exe := buildDebugAlloc(t, string(src), level)
				got, status := runDebugAlloc(t, exe, "")
				if status != 0 || got != string(want) {
					t.Errorf("exit status %d, output:\n%s\nwant:\n%s", status, got, want)
				}
			})
		}
	}
}

// TestMapTasks runs the program that shares a map between puroutines under
// a mutex and hands maps from one puroutine to another on a channel, with
// the debug allocator and its leak report on, at -O0 and -O2, with one, two
// and many processors: counts turn atomic when the first puroutine starts,
// and every map is freed once, by whoever drops the last reference.
func TestMapTasks(t *testing.T) {
	testguard.Parallel(t)
	file := filepath.Join("..", "..", "testdata", "lib", "mapconc")
	src, err := os.ReadFile(file + ".p")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(file + ".out")
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			exe := buildDebugAlloc(t, string(src), level)
			for _, procs := range []string{"1", "2", "24"} {
				t.Run("procs"+procs, func(t *testing.T) {
					testguard.Parallel(t)
					for range 3 {
						got, status := runDebugAlloc(t, exe, "", "PLUTO_MAXPROCS="+procs)
						if status != 0 || got != string(want) {
							t.Fatalf("exit status %d, output:\n%s\nwant:\n%s", status, got, want)
						}
					}
				})
			}
		})
	}
}

// TestMapSeeds checks the hash seeds of maps: in hosted builds each map is
// seeded from random bytes the system gives the process, so the order a
// range visits keys in differs between runs (Go randomises it too); a
// program that sets the seed (as a kernel without entropy would leave the
// fixed default) gets the same order in every run.
func TestMapSeeds(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/rt"

func main() {
	if n, _ := rt.Args(); n > 1 {
		rt.SetMapSeed(12345)
	}
	m := map[int]bool{}
	for i := 0; i < 64; i++ {
		m[i] = true
	}
	s := ""
	for k := range m {
		s += string(rune('0' + k%10))
	}
	println(s)
}
`
	exe := buildDebugAlloc(t, src, 2)
	orders := map[string]bool{}
	for range 6 {
		got, status := runDebugAlloc(t, exe, "")
		if status != 0 {
			t.Fatalf("exit status %d: %s", status, got)
		}
		orders[got] = true
	}
	if len(orders) < 2 {
		t.Errorf("six runs ranged in one order: %v", orders)
	}
	fixed := map[string]bool{}
	for range 4 {
		got, _ := runDebugAllocArgs(t, exe, "seed")
		fixed[got] = true
	}
	if len(fixed) != 1 {
		t.Errorf("runs with a fixed seed ranged in %d orders: %v", len(fixed), fixed)
	}
}

// TestMapHoldElision checks that maps are counted like strings and boxes
// (language spec section 6.10.1): a map passed down is borrowed without
// counting, and one read from memory a call could change is held only
// across calls that may release it.
func TestMapHoldElision(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

type holder struct {
	m map[string]int
}

//pu:noinline
func get(m map[string]int, k string) int { return m[k] }

//pu:noinline
func pure(h *holder) int { return get(h.m, "a") + len(h.m) }

//pu:noinline
func replace(h *holder, m map[string]int) int {
	h.m = map[string]int{"b": 2}
	return m["a"]
}

//pu:noinline
func risky(h *holder) int { return replace(h, h.m) }

func main() {
	h := holder{m: map[string]int{"a": 1}}
	println(pure(&h), risky(&h), len(h.m))
}
`
	pure := listFunc(t, src, "main.pure")
	for _, count := range []string{"·release", "xadd64", "-32("} {
		if strings.Contains(pure, count) {
			t.Errorf("main.pure counts the map it passes (%s):\n%s", count, pure)
		}
	}
	if risky := listFunc(t, src, "main.risky"); !strings.Contains(risky, "-32(") {
		t.Errorf("main.risky does not keep the map it passes alive:\n%s", risky)
	}
	for _, level := range []int{0, 2} {
		exe := buildDebugAlloc(t, src, level)
		if got, status := runDebugAlloc(t, exe, ""); status != 0 || got != "2 1 1\n" {
			t.Errorf("-O%d: exit status %d, output %q", level, status, got)
		}
	}
}
