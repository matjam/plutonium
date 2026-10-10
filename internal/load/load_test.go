package load

import "testing"

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
