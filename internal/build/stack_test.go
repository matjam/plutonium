package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestMainStackOverflow checks that running off the end of a stack reserved
// with //pu:stack lands in its guard region and is reported as a stack
// overflow, like an overflow of the system's stack.
func TestMainStackOverflow(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

//pu:noinline
func down(n int) int {
	var pad [64]int
	pad[n%64] = n
	return down(n+1) + pad[0]
}

//pu:stack 1M
func main() {
	println(down(0))
}
`
	for _, level := range []int{0, 2} {
		out, status := compileAndRun(t, src, level)
		if status != 2 || !strings.HasPrefix(out, "panic: runtime error: stack overflow\n") {
			t.Errorf("-O%d: exit status %d, output starts %q; want 2 and a stack overflow", level, status, firstLine(out))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestMainStackErrors checks that malformed or misplaced //pu:stack
// directives stop the build.
func TestMainStackErrors(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, src, want string }{
		{"missing size", "package main\n\n//pu:stack\nfunc main() {}\n", "//pu:stack: missing size"},
		{"bad size", "package main\n\n//pu:stack lots\nfunc main() {}\n", `//pu:stack: invalid size "lots"`},
		{"bad suffix", "package main\n\n//pu:stack 12T\nfunc main() {}\n", `invalid size "12T"`},
		{"too small", "package main\n\n//pu:stack 4K\nfunc main() {}\n", "smaller than 64K"},
		{"too large", "package main\n\n//pu:stack 65G\nfunc main() {}\n", "larger than 64G"},
		{"overflowing", "package main\n\n//pu:stack 99999999999999999G\nfunc main() {}\n", "larger than 64G"},
		{"not main", "package main\n\n//pu:stack 1M\nfunc f() {}\n\nfunc main() { f() }\n", "//pu:stack applies only to func main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := buildError(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
	t.Run("freestanding", func(t *testing.T) {
		testguard.Parallel(t)
		dir := t.TempDir()
		src := "package main\n\n//pu:stack 1M\nfunc main() {}\n"
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog.o"), Stdlib: plutonium.Stdlib(), Freestanding: true}
		err := build.Build(cfg)
		if err == nil || !strings.Contains(err.Error(), "not supported in freestanding builds") {
			t.Fatalf("got error %v, want the freestanding error", err)
		}
	})
}
