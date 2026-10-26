package build_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runProg runs an executable and returns its combined output and status.
func runProg(t *testing.T, path string) (string, int) {
	t.Helper()
	cmd := exec.Command(path)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		if exitErr.ExitCode() < 0 {
			t.Fatalf("program died: %v\n%s", err, out.String())
		}
		return out.String(), exitErr.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return out.String(), 0
}

func needCC(t *testing.T) {
	t.Helper()
	cc := strings.Fields(os.Getenv("CC"))
	if len(cc) == 0 {
		cc = []string{"cc"}
	}
	if _, err := exec.LookPath(cc[0]); err != nil {
		t.Skip("no C compiler")
	}
}
