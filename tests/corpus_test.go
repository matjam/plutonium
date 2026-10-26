package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// groups are the corpus directories under tests/. Each case is a
// directory holding a program (its .p files, and subdirectories for the
// other packages of a module) and files that say how to build it and
// what the compiler must print:
//
//	default.out  the whole output of the build, sanitized (see sanitize)
//	brief.out    the same with --brief; optional
//	flags        extra arguments, one per line, before the package
//	command      the subcommand and its arguments in place of "build -o OUT"
//	env          KEY=VALUE lines added to the environment
//	status       the expected exit status; 1 when absent
//	knownfail    a note on why the compiler under test does not pass yet
//
// A case with a knownfail note is skipped when it fails and reported when
// it passes, so that the note is removed once the compiler is fixed.
var groups = []string{"race", "types2", "escape", "moves", "lower"}

func TestCorpus(t *testing.T) {
	for _, g := range groups {
		t.Run(g, func(t *testing.T) {
			cases, err := os.ReadDir(g)
			if os.IsNotExist(err) {
				t.Skip("no cases")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cases {
				if !c.IsDir() {
					continue
				}
				dir := filepath.Join(g, c.Name())
				t.Run(c.Name(), func(t *testing.T) {
					t.Parallel()
					runCase(t, dir)
				})
			}
		})
	}
}

// runCase builds the case in dir once per output file it has and
// compares the results.
func runCase(t *testing.T, dir string) {
	note := readOpt(t, dir, "knownfail")
	var fails []string
	for _, mode := range []string{"default", "brief"} {
		want, err := os.ReadFile(filepath.Join(dir, mode+".out"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got, status := buildCase(t, dir, mode == "brief")
		wantStatus := 1
		if s := strings.TrimSpace(readOpt(t, dir, "status")); s != "" {
			if wantStatus, err = strconv.Atoi(s); err != nil {
				t.Fatalf("status: %v", err)
			}
		}
		switch {
		case got == string(want) && status == wantStatus:
			continue
		case *update && note == "":
			if err := os.WriteFile(filepath.Join(dir, mode+".out"), []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			if status != wantStatus {
				t.Errorf("%s: exit status %d, want %d (not updated)", mode, status, wantStatus)
			}
			continue
		}
		fails = append(fails, fmt.Sprintf("%s: exit status %d (want %d)\n--- got:\n%s--- want:\n%s", mode, status, wantStatus, got, want))
	}
	switch {
	case note != "" && len(fails) > 0:
		t.Skipf("known failure: %s", strings.TrimSpace(note))
	case note != "":
		t.Errorf("passes but is marked knownfail (%s): remove %s", strings.TrimSpace(note), filepath.Join(dir, "knownfail"))
	}
	for _, f := range fails {
		t.Error(f)
	}
}

// buildCase copies the case's program to a fresh directory and runs the
// compiler on it, returning the sanitized output and the exit status.
func buildCase(t *testing.T, dir string, brief bool) (string, int) {
	t.Helper()
	work := t.TempDir()
	copyTree(t, dir, work)
	args := []string{"build", "-o", filepath.Join(t.TempDir(), "a.out")}
	if c := strings.Fields(readOpt(t, dir, "command")); len(c) > 0 {
		args = c
	}
	args = append(args, lines(readOpt(t, dir, "flags"))...)
	if brief {
		args = append(args, "--brief")
	}
	args = append(args, ".")
	out, status := runPluto(t, work, lines(readOpt(t, dir, "env")), args...)
	return sanitize(out, work), status
}

// readOpt returns the contents of the optional file name in dir.
func readOpt(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// lines returns the non-empty lines of s.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
