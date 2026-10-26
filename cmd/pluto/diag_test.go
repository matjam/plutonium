package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
)

var update = flag.Bool("update", false, "rewrite the expected output of TestDiagnostics")

// TestDiagnostics builds each program in testdata/diag and checks the
// whole error output, in the default form (each error with its
// explanation) against default.out and with --brief against brief.out. A
// file named flags adds build flags, one per line; a file named command
// replaces the subcommand and its leading arguments ("test"); a file named
// env sets environment variables, one KEY=VALUE per line; and
// subdirectories are copied, for programs of several packages. Run with -update to
// rewrite the expected output after changing an explanation, and read the
// diff.
func TestDiagnostics(t *testing.T) {
	dirs, err := filepath.Glob("testdata/diag/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no test programs")
	}
	for _, src := range dirs {
		t.Run(filepath.Base(src), func(t *testing.T) {
			dir := copyProgram(t, src)
			args := []string{"build", "-o", filepath.Join(t.TempDir(), "a.out")}
			if cmd, err := os.ReadFile(filepath.Join(src, "command")); err == nil {
				// "command" names another subcommand, such as test.
				args = strings.Fields(string(cmd))
			}
			if flags, err := os.ReadFile(filepath.Join(src, "flags")); err == nil {
				args = append(args, strings.Fields(string(flags))...)
			}
			if env, err := os.ReadFile(filepath.Join(src, "env")); err == nil {
				// "env" sets environment variables, one KEY=VALUE per line,
				// such as PLUTO_ESCAPE=new.
				for _, line := range strings.Split(strings.TrimSpace(string(env)), "\n") {
					k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
					if !ok {
						t.Fatalf("env: %q is not KEY=VALUE", line)
					}
					t.Setenv(k, v)
				}
			}
			for _, mode := range []string{"default", "brief"} {
				a := append([]string(nil), args...)
				if mode == "brief" {
					a = append(a, "--brief")
				}
				out, status := runPluto(t, dir, append(a, ".")...)
				if status != 1 {
					t.Errorf("%s: exit status %d, want 1", mode, status)
				}
				checkGolden(t, filepath.Join(src, mode+".out"), sanitize(out, dir))
			}
		})
	}
}

// TestBriefEnv checks that $PLUTO_BRIEF turns on brief errors, that
// --brief=false overrides it, and that pluto test takes -brief too.
func TestBriefEnv(t *testing.T) {
	dir := writeProgram(t, "package main\n\nfunc main() {\n\tvar x int = \"a\"\n\t_ = x\n}\n")
	t.Setenv("PLUTO_BRIEF", "1")
	out, _ := runPluto(t, dir, "build", "-o", filepath.Join(t.TempDir(), "a.out"), ".")
	if strings.Contains(out, "pluto explain") || !strings.Contains(out, "main.p:4:14: cannot use") {
		t.Errorf("PLUTO_BRIEF=1: output\n%s", out)
	}
	out, _ = runPluto(t, dir, "build", "--brief=false", "-o", filepath.Join(t.TempDir(), "a.out"), ".")
	if !strings.Contains(out, "pluto explain E0023") {
		t.Errorf("--brief=false: output\n%s", out)
	}
	out, _ = runPluto(t, dir, "test", "-brief=false", ".")
	if !strings.Contains(out, "pluto explain E0023") {
		t.Errorf("test -brief=false: output\n%s", out)
	}
	t.Setenv("PLUTO_BRIEF", "")
	out, _ = runPluto(t, dir, "test", "-brief", ".")
	if strings.Contains(out, "pluto explain") {
		t.Errorf("test -brief: output\n%s", out)
	}
}

// TestExplain checks that pluto explain prints the explanation of every
// code in the catalog, and lists them all without arguments.
func TestExplain(t *testing.T) {
	var list bytes.Buffer
	if err := explain(&list, nil); err != nil {
		t.Fatal(err)
	}
	codes := diag.Codes()
	if n := strings.Count(list.String(), "\n"); n != len(codes) {
		t.Errorf("list has %d lines, want %d", n, len(codes))
	}
	for _, c := range codes {
		var b bytes.Buffer
		if err := explain(&b, []string{string(c)}); err != nil {
			t.Errorf("%s: %v", c, err)
			continue
		}
		if !strings.HasPrefix(b.String(), string(c)+": "+diag.Lookup(c).Name+"\n") {
			t.Errorf("%s: explanation starts %q", c, firstLine(b.String()))
		}
	}
	if os.Getenv("PLUTO_TEST_COMPILER") != "" {
		// The compiler under test must print what explain prints here, for
		// the list and for every code.
		if out, status := runPluto(t, ".", "explain"); status != 0 || out != list.String() {
			t.Errorf("pluto explain: status %d, output differs from explain's:\n%s", status, out)
		}
		args := []string{"explain"}
		for _, c := range codes {
			args = append(args, string(c))
		}
		var all bytes.Buffer
		if err := explain(&all, args[1:]); err != nil {
			t.Fatal(err)
		}
		if out, status := runPluto(t, ".", args...); status != 0 || out != all.String() {
			t.Errorf("pluto explain <every code>: status %d, output differs from explain's", status)
		}
	}
	out, status := runPluto(t, ".", "explain", "e23", "3213")
	if status != 0 || !strings.HasPrefix(out, "E0023: IncompatibleAssign\n") || !strings.Contains(out, "\nE3213: MapOfOwner\n") {
		t.Errorf("pluto explain e23 3213: status %d, output\n%s", status, out)
	}
	out, status = runPluto(t, ".", "explain", "E7777")
	if status != 1 || !strings.Contains(out, `no such error code "E7777"`) {
		t.Errorf("pluto explain E7777: status %d, output\n%s", status, out)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// copyProgram copies the files of a test program to a new directory, away
// from any pluto.mod above the source tree.
func copyProgram(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, src, dir)
	return dir
}

// copyTree copies the files of src, and of its subdirectories (the packages
// of a module), to dst.
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
		if strings.HasSuffix(e.Name(), ".out") || e.Name() == "flags" || e.Name() == "command" {
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

func checkGolden(t *testing.T, file, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (run with -update to rewrite it)\ngot:\n%s\nwant:\n%s", file, got, want)
	}
}
