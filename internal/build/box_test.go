package build_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestBoxesLeakFree runs the programs that make, move, share and drop
// owned, shared, synced and weak references, owning interfaces and owner
// unions (language spec sections 5.6 and 6.5) with the debug allocator and
// its leak report on, at -O0 and -O2: every object must be dropped and its
// memory freed exactly once, on every path out of a scope, through counts
// and weak references, across tasks and through rt.Goexit.
func TestBoxesLeakFree(t *testing.T) {
	testguard.Parallel(t)
	for _, prog := range []string{"plutonium/boxes", "plutonium/newexpr", "plutonium/owniface", "plutonium/ownunion"} {
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
