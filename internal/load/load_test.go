package load

import (
	"slices"
	"testing"
)

// TestInitOrder checks the order of package initialization (language spec
// section 15.3) on a graph of packages: pluto/rt, pluto/alloc, the Preload
// packages and their imports first, then the rest by import path, each
// after its imports.
func TestInitOrder(t *testing.T) {
	l := newLoader(&Config{Preload: []string{"pluto/alloc/debug"}})
	add := func(path string, imports ...string) {
		p := &Package{Path: path}
		for _, ip := range imports {
			p.Imports = append(p.Imports, l.packages[ip])
		}
		l.packages[path] = p
		l.order = append(l.order, p) // as loaded: imports first
	}
	add("pluto/rt")
	add("pluto/alloc", "pluto/rt")
	add("pluto/unicode/utf8")
	add("pluto/alloc/debug", "pluto/alloc", "pluto/unicode/utf8")
	add("pluto/strings", "pluto/unicode/utf8")
	add("example.com/m/d")
	add("example.com/m/a", "example.com/m/d", "pluto/strings")
	add("example.com/m/c")
	add("main", "example.com/m/a", "example.com/m/c")
	var got []string
	for _, p := range l.initOrder() {
		got = append(got, p.Path)
	}
	want := []string{
		"pluto/rt", "pluto/alloc", "pluto/unicode/utf8", "pluto/alloc/debug",
		"example.com/m/c", "example.com/m/d", "pluto/strings", "example.com/m/a", "main",
	}
	if !slices.Equal(got, want) {
		t.Errorf("initOrder:\n got %v\nwant %v", got, want)
	}
}

// TestTrimmedName checks the file names recorded in debugging information
// (docs/spec/debugging.md section 2.1).
func TestTrimmedName(t *testing.T) {
	p := &Program{Root: "/home/me/mod"}
	for _, tc := range []struct{ name, want string }{
		{"/home/me/mod/main.p", "./main.p"},
		{"/home/me/mod/cmd/tool/main.p", "./cmd/tool/main.p"},
		{"$PLUTOROOT/pluto/fmt/print.p", "pluto/fmt/print.p"},
		{TestMainFile, TestMainFile},
		{"/home/me/module2/x.p", "/home/me/module2/x.p"}, // outside the root
		{"/home/me/x.p", "/home/me/x.p"},
	} {
		if got := p.TrimmedName(tc.name); got != tc.want {
			t.Errorf("TrimmedName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	// Without a root (testing a standard library package), only standard
	// library names change.
	none := &Program{}
	if got := none.TrimmedName("/home/me/mod/main.p"); got != "/home/me/mod/main.p" {
		t.Errorf("no root: got %q", got)
	}
	if got := none.TrimmedName("$PLUTOROOT/pluto/rt/rt.p"); got != "pluto/rt/rt.p" {
		t.Errorf("no root: got %q", got)
	}
}
