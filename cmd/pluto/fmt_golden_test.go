package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diff"
)

// The tests in this file run the formatter's goldens through the pluto
// binary named by PLUTO_TEST_COMPILER, such as the self-hosted compiler,
// whose fmt has a hidden --harness flag for them. The Go packages' own
// tests check the same goldens in process.

// fmtTestCompiler returns $PLUTO_TEST_COMPILER, skipping the test without it.
func fmtTestCompiler(t *testing.T) string {
	t.Helper()
	compiler := os.Getenv("PLUTO_TEST_COMPILER")
	if compiler == "" {
		t.Skip("PLUTO_TEST_COMPILER is not set")
	}
	return compiler
}

// execFmt runs "pluto fmt" with args on the binary compiler and returns
// its standard output, standard error and exit status.
func execFmt(t *testing.T, compiler string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(compiler, append([]string{"fmt"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		if ee.ExitCode() < 0 {
			t.Fatalf("pluto fmt %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
		}
		return stdout.String(), stderr.String(), ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), 0
}

// execHarness formats file in the --harness mode and returns the result.
func execHarness(t *testing.T, compiler, mode, file string) ([]byte, bool) {
	t.Helper()
	out, errOut, code := execFmt(t, compiler, "", "--harness="+mode, file)
	if code != 0 || errOut != "" {
		t.Errorf("pluto fmt --harness=%s %s: status %d, stderr:\n%s", mode, file, code, errOut)
		return nil, false
	}
	return []byte(out), true
}

// checkFmtGolden formats source in mode, compares the result with golden,
// and, if idempotent, checks that formatting golden gives golden.
func checkFmtGolden(t *testing.T, compiler, mode, source, golden string, idempotent bool) {
	t.Helper()
	res, ok := execHarness(t, compiler, mode, source)
	if !ok {
		return
	}
	gld, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res, gld) {
		t.Errorf("%s", diff.Diff("format("+source+")", res, golden, gld))
		return
	}
	if !idempotent {
		return
	}
	res, ok = execHarness(t, compiler, mode, golden)
	if ok && !bytes.Equal(res, gld) {
		t.Errorf("golden is not idempotent: %s", diff.Diff(golden, gld, "format("+golden+")", res))
	}
}

// TestFmtPrinterGoldens runs internal/syntax/printer's goldens, with the
// modes of its TestFiles table.
func TestFmtPrinterGoldens(t *testing.T) {
	compiler := fmtTestCompiler(t)
	const dir = "../../internal/syntax/printer/testdata"
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
			checkFmtGolden(t, compiler, e.mode, filepath.Join(dir, e.source), filepath.Join(dir, e.golden), e.idempotent)
		})
	}
}

// TestFmtFormatGoldens runs internal/syntax/format's goldens (its
// TestGolden): each input formats to its golden, a fixed point.
func TestFmtFormatGoldens(t *testing.T) {
	compiler := fmtTestCompiler(t)
	inputs, err := filepath.Glob("../../internal/syntax/format/testdata/*.input")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no format testdata/*.input files")
	}
	for _, input := range inputs {
		golden := strings.TrimSuffix(input, ".input") + ".golden"
		t.Run(filepath.Base(input), func(t *testing.T) {
			t.Parallel()
			checkFmtGolden(t, compiler, "format", input, golden, true)
		})
	}
}

// TestFmtRepoFormatted is internal/syntax/format's TestRepoFormatted
// through the binary: every .p file under lib, examples, testdata and
// bench/*/pu is formatted.
func TestFmtRepoFormatted(t *testing.T) {
	compiler := fmtTestCompiler(t)
	const root = "../.."
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
	out, errOut, code := execFmt(t, compiler, "", append([]string{"-l"}, files...)...)
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("pluto fmt -l: status %d; unformatted files (run \"pluto fmt -w lib testdata examples bench\"):\n%s%s", code, out, errOut)
	}
}
