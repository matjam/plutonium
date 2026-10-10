package build_test

import (
	"strings"
	"testing"
)

// TestChunkedMisuse checks the arena.Chunked misuses that stop the
// program. testdata/lib/chunked.p covers the uses that work.
func TestChunkedMisuse(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"stale mark after reset", "a := arena.NewChunked(256)\n\t_ = a.Alloc(100, 8)\n\tm := a.Mark()\n\ta.Reset()\n\ta.Release(m)",
			"panic: arena: Release of a stale mark\n"},
		{"stale mark in same chunk", "a := arena.NewChunked(256)\n\t_ = a.Alloc(10, 8)\n\tm0 := a.Mark()\n\t_ = a.Alloc(10, 8)\n\tm1 := a.Mark()\n\ta.Release(m0)\n\ta.Release(m1)",
			"panic: arena: Release of a stale mark\n"},
		{"nil parent", "a := arena.NewChunked(256, nil)\n\t_ = a.Alloc(1, 1)", "panic: arena: NewChunked with a nil parent allocator\n"},
		{"zero value", "var a arena.Chunked\n\t_ = a.Alloc(1, 1)", "panic: arena: Chunked used before NewChunked\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "package main\n\nimport \"pluto/arena\"\n\nfunc main() {\n\t" + c.body + "\n\tprintln(\"unreachable\")\n}\n"
			out, status := compileAndRun(t, src, 0)
			if status != 2 || !strings.HasPrefix(out, c.want) || strings.Contains(out, "unreachable") {
				t.Errorf("exit status %d, output %q; want 2 and %q first", status, out, c.want)
			}
		})
	}
}
