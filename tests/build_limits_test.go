package tests

import (
	"fmt"
	"strings"
	"testing"
)

// TestNestingLimit checks that an expression nested past the parser's limit
// is a compile error that names the limit and has a code (spec section
// 21.4), never a crash. A deep expression under the limit builds.
func TestNestingLimit(t *testing.T) {
	t.Parallel()
	deep := func(n int) string {
		return "package main\n\nfunc main() {\n\tx := " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + "\n\tprintln(x)\n}\n"
	}
	t.Run("past the limit", func(t *testing.T) {
		t.Parallel()
		errs := buildFails(t, deep(100001))
		if !strings.Contains(errs, "nested too deeply") || !strings.Contains(errs, "limit is 100000") || !strings.Contains(errs, "pluto explain E9002") {
			t.Fatalf("got errors %q, want the nesting limit with code E9002", errs)
		}
	})
	t.Run("under the limit", func(t *testing.T) {
		t.Parallel()
		out, status := compileAndRun(t, []byte(deep(200)), 2)
		if status != 0 || out != "1\n" {
			t.Fatalf("exit status %d, output %q; want 0 and 1", status, out)
		}
	})
}

// TestTooManyVariants checks that a union with more variants than its
// 16-bit tag can number is a compile error with its code (E2405), and that
// one with exactly 65536 variants is accepted.
func TestTooManyVariants(t *testing.T) {
	t.Parallel()
	union := func(n int) string {
		var b strings.Builder
		b.WriteString("package main\n\ntype U union {\n")
		for i := range n {
			fmt.Fprintf(&b, "\tV%d\n", i)
		}
		b.WriteString("}\n\nfunc main() {\n\tvar u U\n\t_ = u\n}\n")
		return b.String()
	}
	t.Run("past the limit", func(t *testing.T) {
		t.Parallel()
		errs := buildFails(t, union(1<<16+1))
		if !strings.Contains(errs, "union U has 65537 variants") || !strings.Contains(errs, "pluto explain E2405") {
			t.Fatalf("got errors %q, want E2405", errs)
		}
	})
	t.Run("at the limit", func(t *testing.T) {
		t.Parallel()
		dir := writeProgram(t, union(1<<16))
		if out, status := runPluto(t, dir, nil, "vet", "."); status != 0 {
			t.Fatalf("exit status %d:\n%s", status, out)
		}
	})
}
