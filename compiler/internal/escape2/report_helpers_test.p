// Ported from internal/escape2/report/helpers_test.go at the go-cutover tag.
// The Go file was the external package report_test; Plutonium has none, so
// the report tests are in package escape2 (whose Run they call), and
// loadSrc is escape2_test.p's.

package escape2

import (
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape"
	"compiler/internal/escape2/report"
	"compiler/internal/escape2/sched"
)

// newMessages returns the new analysis's diagnostics of a program's own
// package (main.p), rendered as pluto prints them.
func newMessages(t *testing.T, src string) []string {
	t.Helper()
	prog := loadSrc(t, src)
	p, res, err := Run(prog, sched.Options{})
	if err != nil {
		t.Fatalf("escape2: %v", err)
	}
	var out []string
	for _, d := range report.Diagnostics(prog, p, res) {
		if filepath.Base(d.Pos.Filename) == "main.p" {
			out = compat.Append(out, render(d))
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
		out = compat.Append(out, render(d))
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
			if !strings.Contains(compat.Join(compat.Fields(m), " "), f) {
				continue next
			}
		}
		return m
	}
	t.Errorf("no message holds %q; messages:\n%s", compat.Clone(fragments), compat.Join(msgs, "\n"))
	return ""
}

var _ = diag.ReturnFrameView
