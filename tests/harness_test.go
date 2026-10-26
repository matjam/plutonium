// Package tests runs the compiler's corpus against a pluto binary. It
// only execs the compiler, so it outlives the Go implementation: the
// compiler is $PLUTO_TEST_COMPILER, or else the self-hosted build at the
// repository root (./pluto, see AGENTS.md "Self-hosted compiler").
package tests

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the expected output of the corpus cases that fail")

// compiler returns the absolute path of the pluto binary under test,
// skipping the test when there is none.
func compiler(t *testing.T) string {
	t.Helper()
	c := os.Getenv("PLUTO_TEST_COMPILER")
	if c == "" {
		c = filepath.Join("..", "pluto")
		if _, err := os.Stat(c); err != nil {
			t.Skip("no compiler: set PLUTO_TEST_COMPILER or build the self-hosted compiler as ./pluto (AGENTS.md, \"Self-hosted compiler\")")
		}
	}
	abs, err := filepath.Abs(c)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// libRoot is the repository's standard library, which every build uses.
func libRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runPluto runs pluto with args in dir, with env added to the
// environment, and returns its combined output and exit status.
func runPluto(t *testing.T, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(compiler(t), args...)
	cmd.Dir = dir
	// PLUTO_BRIEF is cleared so that only --brief chooses the form.
	cmd.Env = append(os.Environ(), "PLUTOROOT="+libRoot(t), "PLUTO_BRIEF=")
	cmd.Env = append(cmd.Env, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return out.String(), ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return out.String(), 0
}

// copyTree copies the files of a case directory, and of its
// subdirectories (the packages of a module), to dst, leaving out the
// files that describe the case.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := os.Mkdir(filepath.Join(dst, e.Name()), 0o755); err != nil {
				t.Fatal(err)
			}
			copyTree(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
			continue
		}
		if strings.HasSuffix(e.Name(), ".out") || caseFiles[e.Name()] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// caseFiles are the files of a case directory that describe the case
// rather than belong to its program.
var caseFiles = map[string]bool{"flags": true, "command": true, "env": true, "status": true, "knownfail": true}

var (
	workDir   = regexp.MustCompile(`\S*pluto-build-\d+`)
	codeAddr  = regexp.MustCompile(`\(\.text\+0x[0-9a-f]+\)`)
	ldVersion = regexp.MustCompile(`(?m)^\t(/usr/bin/)?ld(\.bfd)?: `)
)

// sanitize replaces what changes from run to run in out: the program's
// directory, the temporary work directory and code offsets.
func sanitize(out, dir string) string {
	out = strings.ReplaceAll(out, dir, "$DIR")
	out = workDir.ReplaceAllString(out, "$$WORK")
	out = codeAddr.ReplaceAllString(out, "(.text+OFFSET)")
	return ldVersion.ReplaceAllString(out, "\tld: ")
}
