package main_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "version")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "pluto") {
		t.Errorf("expected 'pluto' in version output, got: %s", out)
	}
}

func TestBuildHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "build", "--help")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build --help failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "-o") {
		t.Errorf("expected '-o' flag in build help, got: %s", out)
	}
}
