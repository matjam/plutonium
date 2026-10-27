package tests

import (
	"bytes"
	"fmt"
	"go/ast"
	goformat "go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSameAsGoFormat checks that plain Go formats exactly as go/format
// formats it. The corpus is Go's own go/... sources and printer and gofmt
// test data, and every .go and .p file of this repository that go/format
// accepts. pluto fmt formats them all in one run, as .p files in a
// directory of their own.
func TestSameAsGoFormat(t *testing.T) {
	t.Parallel()
	pluto := compiler(t)
	var files []string
	if goroot := goRoot(); goroot != "" {
		files = append(files, filesUnder(t, filepath.Join(goroot, "src", "go"), ".go")...)
		files = append(files, filesUnder(t, filepath.Join(goroot, "src", "go", "printer", "testdata"), ".input", ".golden")...)
		files = append(files, filesUnder(t, filepath.Join(goroot, "src", "cmd", "gofmt", "testdata"), ".input", ".golden")...)
	} else {
		t.Log("go env GOROOT failed; checking only this repository's files")
	}
	files = append(files, filesUnder(t, "..", ".go", ".p")...)

	dir := t.TempDir()
	type input struct {
		file string // the original
		want []byte // as go/format formats it
	}
	inputs := map[string]input{} // by the name of the copy in dir
	var fragments []input        // statement lists, which pluto fmt reads from standard input
	goStmts := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := goformat.Source(src)
		if err != nil {
			continue // not Go, or a deliberately broken test input
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
		if err != nil {
			// go/format accepts a list of declarations or statements
			// without a package clause; so does pluto fmt, but only on
			// standard input.
			fragments = append(fragments, input{file, want})
			continue
		}
		if hasGoStmt(f) {
			// Plutonium starts a task with task, not go: E1012, section
			// 12.12. pluto fmt reports the go statement as an error.
			goStmts++
			continue
		}
		name := fmt.Sprintf("f%05d.p", len(inputs))
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
		inputs[name] = input{file, want}
	}
	if len(inputs) < 100 {
		t.Fatalf("found only %d Go files", len(inputs))
	}

	// A file that pluto fmt cannot parse is reported on standard error as
	// "path:line:col: message" and left as it is.
	cmd := exec.Command(pluto, "fmt", "-w", dir)
	cmd.Env = append(os.Environ(), "PLUTOROOT="+libRoot(t))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	failed := map[string]string{}
	errLine := regexp.MustCompile(`(f\d{5}\.p):\d+(?::\d+)?: (.*)`)
	for _, line := range strings.Split(stderr.String(), "\n") {
		if m := errLine.FindStringSubmatch(line); m != nil {
			if _, seen := failed[m[1]]; !seen {
				failed[m[1]] = m[2]
			}
		} else if strings.TrimSpace(line) != "" {
			t.Errorf("pluto fmt: %s", line)
		}
	}
	if runErr != nil && len(failed) == 0 {
		t.Fatalf("pluto fmt -w: %v\n%s", runErr, stderr.String())
	}

	compared := 0
	for name, in := range inputs {
		if msg, ok := failed[name]; ok {
			// The only Go that Plutonium does not parse uses one of its
			// keywords, such as packed, as an identifier.
			src, _ := os.ReadFile(in.file)
			if !bytes.Contains(src, []byte("packed")) {
				t.Errorf("%s: go/format accepts it but pluto fmt fails: %s", in.file, msg)
			}
			continue
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		compared++
		if !bytes.Equal(got, in.want) {
			t.Errorf("%s: output differs from go/format:\n%s", in.file, firstDifference(in.want, got))
		}
	}
	for _, in := range fragments {
		src, err := os.ReadFile(in.file)
		if err != nil {
			t.Fatal(err)
		}
		out, errOut, code := runFmt(t, string(src))
		if code != 0 {
			if !bytes.Contains(src, []byte("packed")) {
				t.Errorf("%s: go/format accepts it but pluto fmt fails on standard input: %s", in.file, errOut)
			}
			continue
		}
		compared++
		if out != string(in.want) {
			t.Errorf("%s: output differs from go/format:\n%s", in.file, firstDifference(in.want, []byte(out)))
		}
	}
	if compared < 100 {
		t.Errorf("compared only %d files", compared)
	}
	t.Logf("compared %d files with go/format (%d of them fragments); left out %d with a go statement", compared, len(fragments), goStmts)
}

// hasGoStmt reports whether f has a go statement.
func hasGoStmt(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			found = true
		}
		return !found
	})
	return found
}

// firstDifference shows the first line where got differs from want.
func firstDifference(want, got []byte) string {
	w, g := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		if i >= len(w) || i >= len(g) || w[i] != g[i] {
			return fmt.Sprintf("line %d:\n\tgo/format: %q\n\tpluto fmt: %q", i+1, at(w, i), at(g, i))
		}
	}
	return "(no line differs)"
}

// at returns lines[i], or "" past the end.
func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// filesUnder lists the files under dir with one of the given suffixes,
// leaving out hidden directories such as .git.
func filesUnder(t *testing.T, dir string, suffixes ...string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		for _, s := range suffixes {
			if strings.HasSuffix(path, s) {
				files = append(files, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func goRoot() string {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
