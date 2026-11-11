package tests

import (
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
