package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestChannelsLeakFree runs the program that makes, copies, sends and drops
// channels (language spec section 4.8) with the debug allocator and its
// leak report on, at -O0 and -O2, with one, two and many processors: every
// channel must be freed exactly once, with the values still in its buffer
// and its timer, through copies, struct fields, containers, channels of
// channels, puroutine arguments and a time.After in a select loop.
func TestChannelsLeakFree(t *testing.T) {
	testguard.Parallel(t)
	const prog = "lib/chancount"
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

// TestChanHoldElision checks that a channel borrowed from memory a call
// could change is passed without counting when no call it is held across
// can release a reference (opt.ElideStringHolds), and is counted when one
// can: here a callee that overwrites the channel's source, which would
// otherwise free the channel while it is in use.
func TestChanHoldElision(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

type holder struct {
	c chan int
	s chan string
}

//pu:noinline
func size(c chan int) int { return cap(c) - len(c) }

//pu:noinline
func replace(h *holder, c chan int) int {
	h.c = make(chan int, 1)
	return cap(c)
}

//pu:noinline
func pure(h *holder) int { return size(h.c) + size(h.c) }

//pu:noinline
func risky(h *holder) int { return replace(h, h.c) }

//pu:noinline
func send(h *holder) { h.s <- "x" }

func main() {
	var h holder
	h.c = make(chan int, 5)
	h.s = make(chan string, 1)
	println(pure(&h), risky(&h), cap(h.c))
	send(&h)
	println(<-h.s)
}
`
	for _, name := range []string{"main.pure", "main.send"} {
		if list := listFunc(t, src, name); strings.Contains(list, "xadd64") || strings.Contains(list, "chanFree") {
			t.Errorf("%s counts the channel it uses:\n%s", name, list)
		}
	}
	// The count is at offset 136 of the channel (abi.md section 12.6).
	if risky := listFunc(t, src, "main.risky"); !strings.Contains(risky, "136(") || !strings.Contains(risky, "chanFree") {
		t.Errorf("main.risky does not keep the channel it passes alive:\n%s", risky)
	}
	for _, level := range []int{0, 2} {
		exe := buildDebugAlloc(t, src, level)
		if got, status := runDebugAlloc(t, exe, ""); status != 0 || got != "10 5 1\nx\n" {
			t.Errorf("-O%d: exit status %d, output %q", level, status, got)
		}
	}
}
