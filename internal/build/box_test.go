package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestBoxHoldElision checks that a shared or synced reference borrowed from
// memory a call could change is passed without counting when no call it is
// held across can release a reference (opt.ElideStringHolds), and is
// counted when one can: here a callee that overwrites the reference's
// source, which would otherwise free the object while it is in use.
func TestBoxHoldElision(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/alloc"

type point struct{ x, y int }

type holder struct {
	p shared[point]
	q synced[point]
}

//pu:noinline
func get(p shared[point]) int { return p.x + p.y }

//pu:noinline
func getSynced(q synced[point]) int { return q.x - q.y }

//pu:noinline
func replace(h *holder, p shared[point]) int {
	h.p = alloc.Shared[point]()
	return p.x
}

//pu:noinline
func pure(h *holder) int { return get(h.p) + getSynced(h.q) }

//pu:noinline
func risky(h *holder) int { return replace(h, h.p) }

func main() {
	var h holder
	h.p = alloc.Shared[point]()
	h.p.x = 5
	h.q = alloc.Synced[point]()
	h.q.x = 9
	println(pure(&h), risky(&h), h.p.x)
}
`
	pure := listFunc(t, src, "main.pure")
	for _, count := range []string{"·release", "xadd64", "-32("} {
		if strings.Contains(pure, count) {
			t.Errorf("main.pure counts the references it passes (%s):\n%s", count, pure)
		}
	}
	// The count is at -32 from the object (abi.md section 1.2).
	if risky := listFunc(t, src, "main.risky"); !strings.Contains(risky, "-32(") {
		t.Errorf("main.risky does not keep the reference it passes alive:\n%s", risky)
	}
	for _, level := range []int{0, 2} {
		exe := buildDebugAlloc(t, src, level)
		if got, status := runDebugAlloc(t, exe, ""); status != 0 || got != "14 5 0\n" {
			t.Errorf("-O%d: exit status %d, output %q", level, status, got)
		}
	}
}

// TestBoxesLeakFree runs the programs that make, move, share and drop
// owned, shared, synced and weak references, owning interfaces and owner
// unions (language spec sections 7.19 and 18.7) with the debug allocator and
// its leak report on, at -O0 and -O2: every object must be dropped and its
// memory freed exactly once, on every path out of a scope, through counts
// and weak references, across tasks and through rt.ExitTask. dropsites and
// dropflow check every place an owner moves or is dropped (sections 18.5
// and 18.6): each constructed owner is dropped once, and a variable or
// place known to hold nothing is not dropped.
func TestBoxesLeakFree(t *testing.T) {
	testguard.Parallel(t)
	for _, prog := range []string{"plutonium/boxes", "plutonium/newexpr", "plutonium/ownany", "plutonium/owniface", "plutonium/ownunion",
		"plutonium/dropsites", "plutonium/dropflow", "plutonium/deferowner", "plutonium/dropbase", "plutonium/containerowners", "plutonium/takecounted"} {
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

// TestBoxTasks runs the program that starts methods of owned and synced
// boxes with go (language spec section 12.12) with the debug allocator and
// its leak report on, at -O0 and -O2, with one, two and many processors: the
// task drops the owned box it was given, and the object of a synced
// box is dropped once, by whoever holds the last reference.
func TestBoxTasks(t *testing.T) {
	testguard.Parallel(t)
	const prog = "plutonium/gobox"
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
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			exe := buildDebugAlloc(t, string(src), level)
			for _, procs := range []string{"1", "2", "24"} {
				t.Run("procs"+procs, func(t *testing.T) {
					testguard.Parallel(t)
					for range 5 {
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
