// Ported from internal/escape2/escape2_test.go at the go-cutover tag.
// t.TempDir is testenv.TempDir. The tests of the modes that ran both
// analyses (ModeFromEnv, Compare, Analyze) went with internal/escape
// (18358443); isInternal and sites, which the other tests use, are the
// copies of what escape2.p had.

package escape2

import (
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

func loadSrc(t *testing.T, src string) *load.Program {
	t.Helper()
	dir := testenv.TempDir(t)
	defer testenv.Remove(dir)
	testenv.WriteFile(t, filepath.Join(dir, "main.p"), src)
	cfg := pu.New(load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return prog
}

const accepted = `package main

func main() {
	x := 1
	p := &x
	*p = 2
}
`

const rejected = `package main

func f() *int {
	x := 1
	return &x
}

func main() { _ = f() }
`

// isInternal reports whether err is, or holds, an internal compiler error:
// a failure of the analysis rather than a verdict. An error that is not a
// diagnostic counts as one.
func isInternal(err error) bool {
	if l, ok := err.(*diag.List); ok {
		for _, d := range *l {
			if d.Code == diag.InternalError {
				return true
			}
		}
		return false
	}
	if d, ok := err.(*diag.Diagnostic); ok {
		return d.Code == diag.InternalError
	}
	return true
}

// sites returns the errors of a verdict other than internal errors, as
// "file:line code".
func sites(err error) map[string]bool {
	out := map[string]bool{}
	add := func(d *diag.Diagnostic) {
		if d.Code == diag.InternalError {
			return
		}
		out[compat.Sprintfb("%s:%d %s", compat.ArgS(d.Pos.Filename), compat.ArgI(int64(d.Pos.Line)), compat.ArgS(string(d.Code)))] = true
	}
	switch e := err.(type) {
	case nil:
	case *diag.List:
		for _, d := range *e {
			add(d)
		}
	case *diag.Diagnostic:
		add(e)
	}
	return out
}

// TestCheckVerdict checks the verdicts of Check: it accepts a program
// without escapes, and rejects one that returns a pointer to a local
// variable with an error at that line, not with an internal error.
func TestCheckVerdict(t *testing.T) {
	if err := Check(loadSrc(t, accepted)); err != nil {
		t.Errorf("accepted program: %v", err)
	}
	err := Check(loadSrc(t, rejected))
	if err == nil {
		t.Fatalf("rejected program accepted")
	}
	if isInternal(err) {
		t.Fatalf("rejected program: internal error: %v", err)
	}
	s := sites(err)
	if len(s) != 1 {
		t.Fatalf("rejected program: %d errors, want 1: %v", len(s), err)
	}
	for k := range s {
		if want := compat.Sprintfb(":5 %s", compat.ArgS(string(diag.ReturnFrameView))); !strings.HasSuffix(k, want) {
			t.Errorf("rejected program: error %s, want one at line 5 with code %s", k, string(diag.ReturnFrameView))
		}
	}
}
