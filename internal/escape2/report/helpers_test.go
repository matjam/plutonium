package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/escape2/report"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// loadSrc loads a one-file main package.
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

// newMessages returns the new analysis's diagnostics of a program's own
// package (main.p), rendered as pluto prints them.
func newMessages(t *testing.T, src string) []string {
	t.Helper()
	prog := loadSrc(t, src)
	p, res, err := escape2.Run(prog, sched.Options{})
	if err != nil {
		t.Fatalf("escape2: %v", err)
	}
	var out []string
	for _, d := range report.Diagnostics(prog, p, res) {
		if filepath.Base(d.Pos.Filename) == "main.p" {
			out = append(out, render(d))
		}
	}
	return out
}

// oldMessages returns internal/escape's escape diagnostics of a program,
// rendered.
func oldMessages(t *testing.T, src string) []string {
	t.Helper()
	_, err := escape.Analyze(loadSrc(t, src))
	var out []string
	for _, d := range escapeDiags(err) {
		out = append(out, render(d))
	}
	return out
}

// wantMessage checks that the messages hold one that contains every
// fragment, and returns it.
func wantMessage(t *testing.T, msgs []string, fragments ...string) string {
	t.Helper()
next:
	for _, m := range msgs {
		for _, f := range fragments {
			if !strings.Contains(strings.Join(strings.Fields(m), " "), f) {
				continue next
			}
		}
		return m
	}
	t.Errorf("no message holds %q; messages:\n%s", fragments, strings.Join(msgs, "\n"))
	return ""
}

var _ = diag.ReturnFrameView
