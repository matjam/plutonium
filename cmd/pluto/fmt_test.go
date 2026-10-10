package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	unformatted = "package p\n\ntype R packed uint8 {\n\ta bits[uint8,4]\n\tbb bits[uint8, 4]\n}\n"
	formatted   = "package p\n\ntype R packed uint8 {\n\ta  bits[uint8, 4]\n\tbb bits[uint8, 4]\n}\n"
)

// fmtTree writes a directory with one unformatted and one formatted file,
// a nested directory, a hidden file and a file that is not Plutonium.
func fmtTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.p":         unformatted,
		"b.p":         formatted,
		"sub/c.p":     unformatted,
		"sub/.hide.p": unformatted,
		"sub/d.go":    unformatted,
	}
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runFmtArgs(opts fmtOptions, stdin string, paths ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := runFmt(opts, paths, strings.NewReader(stdin), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestFmtList(t *testing.T) {
	dir := fmtTree(t)
	out, errOut, code := runFmtArgs(fmtOptions{list: true}, "", dir)
	want := filepath.Join(dir, "a.p") + "\n" + filepath.Join(dir, "sub", "c.p") + "\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("pluto fmt -l: status %d, stdout %q, stderr %q; want stdout %q", code, out, errOut, want)
	}
}

func TestFmtWrite(t *testing.T) {
	dir := fmtTree(t)
	if err := os.Chmod(filepath.Join(dir, "a.p"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runFmtArgs(fmtOptions{write: true, list: true}, "", dir)
	if code != 0 || errOut != "" || !strings.Contains(out, "a.p") {
		t.Fatalf("pluto fmt -l -w: status %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, name := range []string{"a.p", "b.p", "sub/c.p"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != formatted {
			t.Errorf("%s after -w:\n%s", name, got)
		}
	}
	st, err := os.Stat(filepath.Join(dir, "a.p"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("-w changed the mode to %v", st.Mode().Perm())
	}
	for _, name := range []string{"sub/.hide.p", "sub/d.go"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != unformatted {
			t.Errorf("%s was rewritten", name)
		}
	}
	// Formatting again changes nothing.
	out, _, code = runFmtArgs(fmtOptions{list: true}, "", dir)
	if code != 0 || out != "" {
		t.Errorf("second pass: status %d, listed %q", code, out)
	}
}

func TestFmtDiff(t *testing.T) {
	dir := fmtTree(t)
	file := filepath.Join(dir, "a.p")
	out, _, code := runFmtArgs(fmtOptions{diff: true}, "", file, filepath.Join(dir, "b.p"))
	if code != 1 {
		t.Errorf("status %d, want 1", code)
	}
	name := filepath.ToSlash(file)
	for _, want := range []string{"--- " + name + ".orig\n", "+++ " + name + "\n", "-\ta bits[uint8,4]\n", "+\ta  bits[uint8, 4]\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "b.p") {
		t.Errorf("diff mentions the formatted file:\n%s", out)
	}
}

func TestFmtStdin(t *testing.T) {
	for _, args := range [][]string{nil, {"-"}} {
		out, errOut, code := runFmtArgs(fmtOptions{}, unformatted, args...)
		if code != 0 || out != formatted || errOut != "" {
			t.Errorf("stdin %v: status %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	// Standard input may be a list of statements.
	out, _, code := runFmtArgs(fmtOptions{}, "var  r  ring[int,8]\nr.Push(1)\n")
	if code != 0 || out != "var r ring[int, 8]\nr.Push(1)\n" {
		t.Errorf("fragment: status %d, stdout %q", code, out)
	}
	_, errOut, code := runFmtArgs(fmtOptions{write: true}, unformatted, "-")
	if code != 2 || !strings.Contains(errOut, "cannot use -w with standard input") {
		t.Errorf("-w with stdin: status %d, stderr %q", code, errOut)
	}
}

func TestFmtErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.p")
	if err := os.WriteFile(bad, []byte("package p\nfunc {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := runFmtArgs(fmtOptions{list: true}, "", bad, filepath.Join(dir, "missing.p"))
	if code != 2 {
		t.Errorf("status %d, want 2", code)
	}
	for _, want := range []string{bad + ":2:", "missing.p"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
}

// TestFmtPlain prints a formatted file to standard output.
func TestFmtPlain(t *testing.T) {
	dir := fmtTree(t)
	out, _, code := runFmtArgs(fmtOptions{}, "", filepath.Join(dir, "a.p"))
	if code != 0 || out != formatted {
		t.Errorf("status %d, stdout %q", code, out)
	}
}
