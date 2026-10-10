package build_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// TestInlineNotes builds a program with Config.InlineNotes (pluto build
// --inlinenotes) and checks the note for each //pu:inline call that stays
// out of line: a recursive call, a call on a path that panics, a callee
// that never returns, calls past the caller's growth limit and a func
// value; and that at -O0 every such call is reported.
func TestInlineNotes(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`package main

//pu:inline
func add(a, b int) int { return a + b }

//pu:inline
func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

//pu:inline
func fail(msg string) { panic(msg) }

//pu:noinline
func get() int { return 3 }

//pu:noinline
func apply(f func(int, int) int) int { return f(1, 2) }

// big is far larger than an ordinary inlining budget.
//
//pu:inline
func big(x int) int {
`)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "\tif x > %d {\n\t\tx = x*%d + get()\n\t}\n", i, i+3)
	}
	b.WriteString("\treturn x\n}\n\n//pu:noinline\nfunc many() int {\n\tn := 0\n")
	for i := 0; i < 30; i++ {
		b.WriteString("\tn += big(n)\n")
	}
	b.WriteString(`	return n
}

func main() {
	n := get()
	println(add(n, 2), fib(n+5), apply(add), many() != 0)
	if n > 5 {
		panic(add(n, 6))
	}
	if n > 4 {
		fail("too many")
	}
}
`)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := func(level int) string {
		var buf bytes.Buffer
		cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), OptLevel: level, InlineNotes: &buf}
		if err := build.Build(cfg); err != nil {
			t.Fatalf("build: %v", err)
		}
		return strings.ReplaceAll(buf.String(), dir, "$DIR")
	}

	// fib is inlined into main, and the recursive calls in the copies
	// again, until the inliner's rounds run out.
	want := "$DIR/main.p:11:9: note: call to main.fib marked //pu:inline was not inlined: the call is recursive; the inliner stopped after its last round\n" +
		"$DIR/main.p:368:37: note: main.add marked //pu:inline is used as a func value: calls through a func value are not inlined\n" +
		"$DIR/main.p:370:9: note: call to main.add marked //pu:inline was not inlined: the call is on a path that panics\n" +
		"$DIR/main.p:373:3: note: call to main.fail marked //pu:inline was not inlined: it never returns\n"
	// The calls of big in many (lines 333 to 362) are inlined until many
	// reaches its growth limit; the rest stay, the last ones written.
	var got, budget strings.Builder
	for _, line := range strings.SplitAfter(notes(2), "\n") {
		if strings.Contains(line, "main.big") {
			budget.WriteString(line)
		} else {
			got.WriteString(line)
		}
	}
	if got.String() != want {
		t.Errorf("-O2 notes:\n%s\nwant:\n%s", got.String(), want)
	}
	n := strings.Count(budget.String(), "\n")
	last := "$DIR/main.p:362:7: note: call to main.big marked //pu:inline was not inlined: main.many would grow past its limit of 5000 values\n"
	if n == 0 || n == 30 || !strings.HasSuffix(budget.String(), last) {
		t.Errorf("-O2: %d calls of big reported, want some but not all 30, ending with %q; got:\n%s", n, last, budget.String())
	}

	// At -O0 nothing is inlined: 36 calls and the func value.
	o0 := notes(0)
	if n := strings.Count(o0, ": inlining runs only at -O2\n"); n != 36 || strings.Count(o0, "\n") != 37 {
		t.Errorf("-O0: %d calls reported as not inlined, want 36 and the func value; got:\n%s", n, o0)
	}
}
