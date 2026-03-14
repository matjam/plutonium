package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/token"
)

func TestErrorFormatting(t *testing.T) {
	fset := token.NewFileSet()
	fset.AddFile("main.pu", -1, 100)

	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Errorf(token.Pos(2), "unexpected token %q", "chan")

	out := buf.String()
	if out == "" {
		t.Error("expected non-empty diagnostic output")
	}
	if !strings.Contains(out, "error:") {
		t.Errorf("expected 'error:' in output, got: %q", out)
	}
	if !strings.Contains(out, "chan") {
		t.Errorf("expected token in output, got: %q", out)
	}
}

func TestWarnFormatting(t *testing.T) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Warnf(token.NoPos, "something might be wrong")

	out := buf.String()
	if !strings.Contains(out, "warning:") {
		t.Errorf("expected 'warning:' in output, got: %q", out)
	}
}

func TestErrorCount(t *testing.T) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Errorf(token.NoPos, "error one")
	d.Errorf(token.NoPos, "error two")
	if d.ErrorCount() != 2 {
		t.Errorf("got %d errors, want 2", d.ErrorCount())
	}
	if !d.HasErrors() {
		t.Error("expected HasErrors() to be true")
	}
}

func TestWarnCount(t *testing.T) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Warnf(token.NoPos, "warn one")
	if d.WarnCount() != 1 {
		t.Errorf("got %d warns, want 1", d.WarnCount())
	}
}

func TestNoErrors(t *testing.T) {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	if d.HasErrors() {
		t.Error("expected no errors initially")
	}
	if d.ErrorCount() != 0 {
		t.Errorf("got %d errors, want 0", d.ErrorCount())
	}
}

func TestPositionInOutput(t *testing.T) {
	fset := token.NewFileSet()
	f := fset.AddFile("kernel.pu", -1, 200)
	f.AddLine(50) // line 2 starts at offset 50

	var buf bytes.Buffer
	d := diag.New(fset, &buf)
	d.Errorf(f.Pos(51), "test error")

	out := buf.String()
	if !strings.Contains(out, "kernel.pu") {
		t.Errorf("expected filename in output, got: %q", out)
	}
}
