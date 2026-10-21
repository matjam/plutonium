package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestDriverMatchesCmd builds the driver that go2pu translates into
// selfhost/cmd/pluto (testdata/pluto, which parses its command line without
// cobra) and cmd/pluto with Go, and requires the same output and exit status
// from both for command lines of build, run and version: flag errors and
// usage texts, a compile error, a program built and run. The translation's
// own output is checked against cmd/pluto by tools/bootstrap -corpus.
func TestDriverMatchesCmd(t *testing.T) {
	testguard.Parallel(t)
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	work := t.TempDir()
	driver := goBinary(t, "./testdata/pluto")
	cmd := goBinary(t, "../../cmd/pluto")
	bad := filepath.Join(work, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "main.p"), []byte("package main\n\nfunc main() {\n\tvar x int = \"a\"\n\t_ = x\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(work, "a.out")
	hello := filepath.Join(repoRoot, "examples", "07-hello")
	for _, args := range [][]string{
		{"version"},
		{"build", "--help"}, {"run", "-h"},
		{"build", "--bogus"}, {"build", "-O"}, {"build", "-x"}, {"build", "-cx"}, {"build", "-o"}, {"build", "--output"},
		{"build", "a", "b"}, {"run", "a", "b"}, {"build", "-O", "x"}, {"build", "--opt=abc"}, {"build", "--gencheck=x"},
		{"build", "--mode=x"}, {"build", "--target=arm-linux"}, {"build", "--format=pe"}, {"build", "--alloc=x"},
		{"build", "--mode=kernel", "--alloc=debug"}, {"run", "--mode=kernel", hello},
		{"build", "-o", out, bad}, {"build", "--brief", "-o", out, bad}, {"build", "-o", out, filepath.Join(work, "missing")},
		{"build", "-O0", "-g=false", "-o", out, hello}, {"run", "-O0", hello, "--", "x", "y"},
	} {
		want, wantStatus := runDriver(t, cmd, args)
		got, gotStatus := runDriver(t, driver, args)
		if got != want || gotStatus != wantStatus {
			t.Errorf("pluto %s: exit status %d, output\n%s\nwant status %d, output\n%s", strings.Join(args, " "), gotStatus, got, wantStatus, want)
		}
	}
}

// runDriver runs a pluto binary with args and returns its combined output
// and exit status.
func runDriver(t *testing.T, bin string, args []string) (string, int) {
	t.Helper()
	lib, err := filepath.Abs(filepath.Join(repoRoot, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	c := exec.Command(bin, args...)
	c.Env = append(os.Environ(), "PLUTOROOT="+lib, "PLUTO_BRIEF=")
	c.Stdout, c.Stderr = &buf, &buf
	err = c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return buf.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.String(), 0
}
