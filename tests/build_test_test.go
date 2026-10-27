// Ported from internal/build/test_test.go at the go-cutover tag.

package tests

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestBuildTest builds and runs the test binaries of standard library
// packages with _test.p files at each optimisation level (pluto test -v
// -O level). The old test called build.BuildTest; its other half, the output
// of the command, is tested in plutotest_test.go.
func TestBuildTest(t *testing.T) {
	t.Parallel()
	runLine := regexp.MustCompile(`(?m)^=== RUN   (\w+)$`)
	for _, pkg := range []string{"pluto/strings", "pluto/sort", "pluto/path/filepath", "pluto/flag", "pluto/hash"} {
		for _, level := range []int{0, 2} {
			t.Run(pkg+"/O"+strconv.Itoa(level), func(t *testing.T) {
				t.Parallel()
				got, stderr, status := runTestCommand(t, "-v", "-O", strconv.Itoa(level), pkg)
				if status != 0 {
					t.Fatalf("exit status %d\n%s%s", status, got, stderr)
				}
				tests := runLine.FindAllStringSubmatch(got, -1)
				if len(tests) == 0 {
					t.Fatalf("no tests ran for %q:\n%s", pkg, got)
				}
				if !strings.Contains(got, "ok  \t"+pkg+"\t") {
					t.Errorf("no ok line for %q in:\n%s", pkg, got)
				}
				for _, m := range tests {
					if !strings.Contains(got, "--- PASS: "+m[1]+" (") {
						t.Errorf("no PASS line for %s in:\n%s", m[1], got)
					}
				}
				if !strings.Contains(got, "\nPASS\n") {
					t.Errorf("output has no PASS line:\n%s", got)
				}
			})
		}
	}
}

// fatalTest fails a test with Fatalf, which must leave the test function at
// once and let the next test run, and keeps values that live across the
// jump back to the runner in registers and on the stack.
const fatalTest = `package p

import "pluto/testing"

func sum(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		s += i
	}
	return s
}

func TestFatal(t *testing.T) {
	a, b, c := sum(3), sum(4), sum(5)
	t.Run("inner", func(t *testing.T) {
		t.Fatalf("stop %d %d %d", a, b, c)
		t.Error("not reached")
	})
	t.Logf("after %d", a+b+c)
	t.FailNow()
	t.Error("not reached")
}

func TestNext(t *testing.T) {
	t.Log("next ran")
}
`

func TestBuildTestFailNow(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"pluto.mod": "module example.com/p\n", "p.p": "package p\n", "p_test.p": fatalTest})
	// The reports of the subtests come with their parent's, as in Go's.
	want := `=== RUN   TestFatal
=== RUN   TestFatal/inner
    stop 3 6 10
    after 19
--- FAIL: TestFatal (T)
    --- FAIL: TestFatal/inner (T)
=== RUN   TestNext
    next ran
--- PASS: TestNext (T)
FAIL
FAIL	example.com/p	T
FAIL
`
	for _, level := range []int{0, 2} {
		out, status := runPluto(t, dir, nil, "test", "-v", "-O", strconv.Itoa(level))
		if status != 1 {
			t.Errorf("-O%d: exit status %d, want 1", level, status)
		}
		if s := times.ReplaceAllString(out, "T"); s != want {
			t.Errorf("-O%d output:\n%s\nwant:\n%s", level, s, want)
		}
	}
}

func TestBuildTestNoTestFiles(t *testing.T) {
	t.Parallel()
	out, status := runPluto(t, ".", nil, "test", "pluto/bits")
	if status != 0 || !strings.HasPrefix(out, "?   \tpluto/bits\t[no test files]") {
		t.Errorf("pluto test pluto/bits: exit status %d, output %q; want no test files", status, out)
	}
}
