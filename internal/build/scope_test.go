package build_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestScopeTasks runs the programs that start tasks with sync.Scope
// (language spec section 19.4) with the debug allocator and its leak report
// on, at -O0 and -O2, with one, two and many processors: the tasks use the
// frames of the functions that start them, every Scope waits for its tasks
// on every path out of its block (return, break, rt.Goexit), and the owners
// and strings tasks take are released. TestPlutonium runs them with the
// default allocator.
func TestScopeTasks(t *testing.T) {
	testguard.Parallel(t)
	for _, prog := range []string{"scope", "scope_exits"} {
		file := filepath.Join("..", "..", "testdata", "plutonium", prog)
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
}
