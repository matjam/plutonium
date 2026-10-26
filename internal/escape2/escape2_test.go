package escape2

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func loadSrc(t *testing.T, src string) *load.Program {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
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

func TestModeFromEnv(t *testing.T) {
	for v, want := range map[string]Mode{"": ModeNew, "old": ModeOld, "new": ModeNew, "both": ModeBoth} {
		t.Setenv("PLUTO_ESCAPE", v)
		if got, err := ModeFromEnv(); err != nil || got != want {
			t.Errorf("PLUTO_ESCAPE=%q: %v, %v; want %v", v, got, err, want)
		}
	}
	t.Setenv("PLUTO_ESCAPE", "newer")
	if _, err := ModeFromEnv(); err == nil {
		t.Errorf("PLUTO_ESCAPE=newer: no error")
	}
}

func list(sites ...string) error {
	var l diag.List
	for _, s := range sites {
		pos, code, _ := strings.Cut(s, " ")
		parts := strings.Split(pos, ":")
		line, col := 0, 0
		for _, c := range parts[1] {
			line = line*10 + int(c-'0')
		}
		for _, c := range parts[2] {
			col = col*10 + int(c-'0')
		}
		l = append(l, diag.New(token.Position{Filename: parts[0], Line: line, Column: col}, diag.Code(code), "msg"))
	}
	return l.Err()
}

// TestCompare checks verdict and site comparison.
func TestCompare(t *testing.T) {
	prog := &load.Program{}
	if d := Compare(prog, nil, nil); d != nil {
		t.Errorf("both accept: %v", d)
	}
	if d := Compare(prog, list("a.p:1:2 E3001"), list("a.p:1:2 E3001")); d != nil {
		t.Errorf("same errors: %v", d)
	}
	d := Compare(prog, list("a.p:1:2 E3001"), nil)
	if d == nil || !d.VerdictsDiffer() || len(d.OnlyOld) != 1 || len(d.OnlyNew) != 0 {
		t.Fatalf("old rejects, new accepts: %+v", d)
	}
	if s := d.String(); !strings.Contains(s, "verdicts differ: old rejects, new accepts") || !strings.Contains(s, "old only: a.p:1 E3001") {
		t.Errorf("report:\n%s", s)
	}
	d = Compare(prog, list("a.p:1:2 E3001"), list("a.p:3:4 E3002", "a.p:1:2 E3001"))
	if d == nil || d.VerdictsDiffer() || len(d.OnlyNew) != 1 || d.OnlyNew[0].Pos != "a.p:3" {
		t.Errorf("same verdict, extra site: %+v", d)
	}
	d = Compare(prog, nil, errors.New("internal compiler error: escape2: not built"))
	if d == nil || !d.VerdictsDiffer() || d.NewFailed == "" {
		t.Errorf("new fails: %+v", d)
	}
	if s := d.String(); !strings.Contains(s, "new analysis failed") {
		t.Errorf("report:\n%s", s)
	}
}

// TestCompareColumns checks that one error reported at another column of
// the same line is the same site.
func TestCompareColumns(t *testing.T) {
	if d := Compare(&load.Program{}, list("a.p:5:9 E3001"), list("a.p:5:2 E3001")); d != nil {
		t.Errorf("same line, other column: %v", d)
	}
}

// TestAnalyzeModes checks that the old analysis decides the verdict in the
// old and both modes, and that both mode reports a difference to the file
// PLUTO_ESCAPE_DIFF names. While the new analysis is not complete it fails
// on every program, which both mode reports as a difference.
func TestAnalyzeModes(t *testing.T) {
	ok, bad := loadSrc(t, accepted), loadSrc(t, rejected)
	diffFile := filepath.Join(t.TempDir(), "diff.txt")
	t.Setenv("PLUTO_ESCAPE_DIFF", diffFile)
	for _, mode := range []Mode{ModeOld, ModeBoth} {
		calls, err := Analyze(ok, mode)
		if err != nil || calls == nil {
			t.Errorf("mode %d: accepted program: %v", mode, err)
		}
		if _, err := Analyze(bad, mode); err == nil {
			t.Errorf("mode %d: rejected program accepted", mode)
		}
	}
	data, _ := os.ReadFile(diffFile)
	if d := Compare(ok, nil, Check(ok)); d != nil && !strings.Contains(string(data), d.String()) {
		t.Errorf("both mode did not report\n%s\nin PLUTO_ESCAPE_DIFF; it has\n%s", d, data)
	}
}

// TestCheckInternalError checks that a failure of the new analysis is an
// internal compiler error, never acceptance.
func TestCheckInternalError(t *testing.T) {
	err := Check(loadSrc(t, accepted))
	if err == nil {
		t.Skip("the new analysis is complete enough to accept this program")
	}
	if !isInternal(err) && len(sites(err)) == 0 {
		t.Errorf("unexpected error: %v", err)
	}
}
