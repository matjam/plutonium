package tests

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file check pluto fmt: its flags, and the formatter's
// goldens under testdata/fmt through the hidden --harness flag.

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

// runFmt runs "pluto fmt" with args and stdin as its standard input, and
// returns its standard output, standard error and exit status.
func runFmt(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	return runPlutoSplit(t, ".", nil, stdin, append([]string{"fmt"}, args...)...)
}

func TestFmtList(t *testing.T) {
	t.Parallel()
	dir := fmtTree(t)
	out, errOut, code := runFmt(t, "", "-l", dir)
	want := filepath.Join(dir, "a.p") + "\n" + filepath.Join(dir, "sub", "c.p") + "\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("pluto fmt -l: status %d, stdout %q, stderr %q; want stdout %q", code, out, errOut, want)
	}
}

func TestFmtWrite(t *testing.T) {
	t.Parallel()
	dir := fmtTree(t)
	if err := os.Chmod(filepath.Join(dir, "a.p"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runFmt(t, "", "-l", "-w", dir)
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
	out, _, code = runFmt(t, "", "-l", dir)
	if code != 0 || out != "" {
		t.Errorf("second pass: status %d, listed %q", code, out)
	}
}

func TestFmtDiff(t *testing.T) {
	t.Parallel()
	dir := fmtTree(t)
	file := filepath.Join(dir, "a.p")
	out, _, code := runFmt(t, "", "-d", file, filepath.Join(dir, "b.p"))
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
	t.Parallel()
	for _, args := range [][]string{nil, {"-"}} {
		out, errOut, code := runFmt(t, unformatted, args...)
		if code != 0 || out != formatted || errOut != "" {
			t.Errorf("stdin %v: status %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	// Standard input may be a list of statements.
	out, _, code := runFmt(t, "var  r  ring[int,8]\nr.Push(1)\n")
	if code != 0 || out != "var r ring[int, 8]\nr.Push(1)\n" {
		t.Errorf("fragment: status %d, stdout %q", code, out)
	}
	_, errOut, code := runFmt(t, unformatted, "-w", "-")
	if code != 2 || !strings.Contains(errOut, "cannot use -w with standard input") {
		t.Errorf("-w with stdin: status %d, stderr %q", code, errOut)
	}
}

func TestFmtErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.p")
	if err := os.WriteFile(bad, []byte("package p\nfunc {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := runFmt(t, "", "-l", bad, filepath.Join(dir, "missing.p"))
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
	t.Parallel()
	dir := fmtTree(t)
	out, _, code := runFmt(t, "", filepath.Join(dir, "a.p"))
	if code != 0 || out != formatted {
		t.Errorf("status %d, stdout %q", code, out)
	}
}

// execHarness formats file in the --harness mode and returns the result.
func execHarness(t *testing.T, mode, file string) ([]byte, bool) {
	t.Helper()
	out, errOut, code := runFmt(t, "", "--harness="+mode, file)
	if code != 0 || errOut != "" {
		t.Errorf("pluto fmt --harness=%s %s: status %d, stderr:\n%s", mode, file, code, errOut)
		return nil, false
	}
	return []byte(out), true
}

// checkFmtGolden formats source in mode, compares the result with golden,
// and, if idempotent, checks that formatting golden gives golden.
func checkFmtGolden(t *testing.T, mode, source, golden string, idempotent bool) {
	t.Helper()
	res, ok := execHarness(t, mode, source)
	if !ok {
		return
	}
	gld, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res, gld) {
		t.Errorf("format(%s) differs from %s\ngot:\n%s\nwant:\n%s", source, golden, res, gld)
		return
	}
	if !idempotent {
		return
	}
	res, ok = execHarness(t, mode, golden)
	if ok && !bytes.Equal(res, gld) {
		t.Errorf("golden %s is not idempotent; format gives:\n%s", golden, res)
	}
}

// TestFmtPrinterGoldens runs the printer's goldens (testdata/fmt/printer)
// in the modes of internal/syntax/printer's TestFiles table.
func TestFmtPrinterGoldens(t *testing.T) {
	const dir = "testdata/fmt/printer"
	data := []struct {
		source, golden, mode string
		idempotent           bool
	}{
		{"empty.input", "empty.golden", "printer", true},
		{"comments.input", "comments.golden", "printer", false},
		{"comments2.input", "comments2.golden", "printer", true},
		{"alignment.input", "alignment.golden", "printer", true},
		{"linebreaks.input", "linebreaks.golden", "printer", true},
		{"expressions.input", "expressions.golden", "printer", true},
		{"expressions.input", "expressions.raw", "printer-raw", true},
		{"declarations.input", "declarations.golden", "printer", false},
		{"statements.input", "statements.golden", "printer", false},
		{"slow.input", "slow.golden", "printer", true},
		{"go2numbers.input", "go2numbers.golden", "printer", true},
		{"go2numbers.input", "go2numbers.norm", "printer-norm", true},
		{"generics.input", "generics.golden", "printer", true},
		{"gobuild1.input", "gobuild1.golden", "printer", true},
		{"gobuild2.input", "gobuild2.golden", "printer", true},
		{"gobuild3.input", "gobuild3.golden", "printer", true},
		{"gobuild4.input", "gobuild4.golden", "printer", true},
		{"gobuild5.input", "gobuild5.golden", "printer", true},
		{"gobuild6.input", "gobuild6.golden", "printer", true},
		{"gobuild7.input", "gobuild7.golden", "printer", true},
	}
	for _, e := range data {
		t.Run(e.golden, func(t *testing.T) {
			t.Parallel()
			checkFmtGolden(t, e.mode, filepath.Join(dir, e.source), filepath.Join(dir, e.golden), e.idempotent)
		})
	}
}

// TestFmtFormatGoldens runs the formatter's goldens (testdata/fmt/format):
// each input formats to its golden, a fixed point.
func TestFmtFormatGoldens(t *testing.T) {
	inputs, err := filepath.Glob("testdata/fmt/format/*.input")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no testdata/fmt/format/*.input files")
	}
	for _, input := range inputs {
		golden := strings.TrimSuffix(input, ".input") + ".golden"
		t.Run(filepath.Base(input), func(t *testing.T) {
			t.Parallel()
			checkFmtGolden(t, "format", input, golden, true)
		})
	}
}

// TestFmtRepoFormatted checks that every .p file under lib, examples,
// testdata and bench/*/pu is formatted.
func TestFmtRepoFormatted(t *testing.T) {
	const root = ".."
	dirs := []string{filepath.Join(root, "lib"), filepath.Join(root, "examples"), filepath.Join(root, "testdata")}
	bench, err := filepath.Glob(filepath.Join(root, "bench", "*", "pu"))
	if err != nil {
		t.Fatal(err)
	}
	dirs = append(dirs, bench...)
	var files []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".p") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) < 50 {
		t.Fatalf("found only %d .p files; is the repository layout different?", len(files))
	}
	out, errOut, code := runFmt(t, "", append([]string{"-l"}, files...)...)
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("pluto fmt -l: status %d; unformatted files (run \"pluto fmt -w lib testdata examples bench\"):\n%s%s", code, out, errOut)
	}
}
