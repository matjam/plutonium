// Ported from internal/build/or_test.go at the go-cutover tag.

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestOrLeakFree runs testdata/plutonium/orowner.p, whose or statements
// drop owners, strings and shared boxes where their calls fail (language
// spec section 11.27), with the debug allocator and its leak report on, at
// -O0 and -O2: every value the call returned besides the error, and every
// temporary of the statement, is released exactly once on each path.
// TestPlutonium runs it with the default allocator.
func TestOrLeakFree(t *testing.T) {
	t.Parallel()
	file := filepath.Join("..", "testdata", "plutonium", "orowner")
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
			t.Parallel()
			got, status := runDebugAlloc(t, buildDebugAlloc(t, string(src), level), "leaks=fail")
			if status != 0 || got != string(want) {
				t.Fatalf("exit status %d, output:\n%s\nwant:\n%s", status, got, want)
			}
		})
	}
}

// TestOrKernel compiles or statements in kernel mode (language spec
// section 11.27): the statement makes no call and no allocation of its
// own, so the only calls in the function are those it writes.
func TestOrKernel(t *testing.T) {
	t.Parallel()
	const src = `package main

type Code int

func (c Code) Error() string { return "code" }

//pu:noinline
func read(port int) (int, error) {
	if port < 0 {
		return 0, Code(port)
	}
	return port * 2, nil
}

//pu:noinline
func sum(ports []int) (int, error) {
	total := 0
	for _, p := range ports {
		v := read(p) or return
		w := read(v) or {
			if p > 100 {
				continue
			}
			return 0, err
		}
		total += v + w
	}
	if total > 1000 {
		read(-total) or return total, err
	}
	return total, nil
}

func main() {
	n := sum([]int{1, 2}) or panic(err)
	_ = n
}
`
	re := regexp.MustCompile(`call\s+(\S+)`)
	for _, level := range []int{0, 2} {
		listing, errs := kernelCompile(t, src, level)
		if errs != "" {
			t.Fatalf("O%d: %v", level, errs)
		}
		fn := funcListing(t, listing, "main.sum")
		for _, m := range re.FindAllStringSubmatch(fn, -1) {
			// Section 16.7 adds guarded retain/release calls for counted
			// errors, including the indirect dynamic drop adapter.
			if !strings.Contains(m[1], "main.read") && m[1] != "pluto/rt.boxRetain" && m[1] != "pluto/rt.xadd64" && !strings.HasPrefix(m[1], "*%") {
				t.Errorf("O%d: main.sum calls %s:\n%s", level, m[1], fn)
			}
		}
		if !strings.Contains(fn, "main.read") {
			t.Errorf("O%d: main.sum does not call main.read:\n%s", level, fn)
		}
	}
}
