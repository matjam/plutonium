package tests

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file check the parser's errors through pluto doc,
// which loads a package and reports its syntax errors (E1xxx) first.

// parseErrors loads src as a package with pluto doc and returns its
// diagnostics as "line:col: message", the file name left out.
func parseErrors(t *testing.T, src string) []string {
	t.Helper()
	out, status := runPluto(t, writeProgram(t, src), nil, "doc")
	var got []string
	for _, d := range diagnostics(out) {
		_, pos, _ := strings.Cut(d.pos, "main.p:")
		got = append(got, pos+": "+d.msg)
	}
	if status == 0 || len(got) == 0 {
		t.Fatalf("no syntax error: status %d\n%s", status, out)
	}
	return got
}

// TestParseOrErrors checks the syntax errors of the or statement: an
// operand that is not a call (E1013), a clause not on the line of the or,
// and or in a statement that takes no clause, where it is an identifier.
func TestParseOrErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string // the body of a function
		want string // the first error, without the file name
	}{
		{"a := g() + g() or return", "4:7: the operand of or is not a single call"},
		{"b := (f()) or return", "4:7: the operand of or is a call in parentheses; write the call without them"},
		{"c := f().x or return", "4:7: the operand of or is not a call; or must directly follow the call whose error it tests"},
		{"d := x or return", "4:7: the operand of or is not a single call"},
		{"a, b := x, f() or return", "4:10: the operand of or is a list of values; or follows a single call"},
		{"e := (x) or return", "4:7: the operand of or is not a single call"},
		{"f() or\n\treturn", "4:6: or must be followed by its clause on the same line"},
		{"f() or; return", "4:8: or must be followed by a clause: a block, a return, break, continue or goto statement, or a call such as panic(err)"},
		{"f()\n\tor return", "5:2: or must be on the line where its call ends: a newline after the call ends the statement"},
		{"f()\n\tor panic(err)", "5:2: or must be on the line where its call ends: a newline after the call ends the statement"},
		{"var x = f() or return", "4:14: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"defer f() or return", "4:12: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"task f() or return", "4:11: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"n += f() or return", "4:11: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"ch <- f() or return", "4:12: expected end of statement, found or: an or clause follows only a call that stands alone or is on the right of = or :="},
		{"for v := range f() or return {\n\t}", "4:21: expected '{', found or"},
		{"select {\n\tcase v := <-ch or return:\n\t}", "5:17: expected ':', found or"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			got := parseErrors(t, "package p\n\nfunc h() {\n\t"+tt.src+"\n}\n")
			if got[0] != tt.want {
				t.Errorf("%q:\n got %s\nwant %s", tt.src, got[0], tt.want)
			}
		})
	}
}

// TestParseGoStmt checks that a go statement is an error that names task,
// and that it still parses as a task statement so that nothing else is
// reported for it; and that go cannot name a variable.
func TestParseGoStmt(t *testing.T) {
	t.Parallel()
	got := parseErrors(t, "package p\n\nfunc f() {\n\tgo worker(jobs)\n\tgo func() {\n\t}()\n}\n")
	want := []string{
		"4:2: go starts no task here; write: task worker(jobs)",
		"5:2: go starts no task here; write task in place of go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("errors:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	parseErrors(t, "package p\n\nfunc f() {\n\tgo := 1\n}\n")
}

// TestParseDeclErrors checks the syntax errors of the declarations of
// Plutonium's own types, of methods with type parameters and of default
// parameter values: the first error contains the text given.
func TestParseDeclErrors(t *testing.T) {
	t.Parallel()
	const defaultErr = "a default value must follow a single parameter name and its type"
	for _, bad := range []struct{ name, src, want string }{
		{"enum value", "package p\ntype E enum { 1 }", "expected '}'"},
		{"enum missing value", "package p\ntype E enum { A = }", "expected operand"},
		{"enum keyword", "package p\nvar enum = 1", "expected 'IDENT'"},
		{"union value", "package p\ntype U union { 1 }", "expected '}'"},
		{"union empty case", "package p\ntype U packed union uint8 { A case { } }", "expected operand"},
		{"union case", "package p\ntype U packed union uint8 { A case 1 x }", "expected '{'"},
		{"union var", "package p\nvar x union { A int }", "union type must be the type of a type definition"},
		{"interface generic method", "package p\n\ntype I interface {\n\tM[T any](x T) T\n}\n", "interface method must have no type parameters: a method with type parameters is never called through an interface"},
		{"shared default", "package p\n\nfunc f(a, b int = 1) {}\n", defaultErr},
		{"default without type", "package p\n\nfunc f(a = 1, b int) {}\n", defaultErr},
	} {
		t.Run(bad.name, func(t *testing.T) {
			t.Parallel()
			if got := parseErrors(t, bad.src); !strings.Contains(got[0], bad.want) {
				t.Errorf("%q: got error %v, want %q", bad.src, got[0], bad.want)
			}
		})
	}
}

// TestParseRepoSources parses every .p file shipped in the repository under
// lib, examples, testdata and bench, and the command's test programs that
// came from cmd/pluto/testdata, but for the ones named invalid, with pluto
// fmt -l, which reports a file that does not parse.
func TestParseRepoSources(t *testing.T) {
	t.Parallel()
	root := ".."
	var files []string
	for _, dir := range []string{"lib", "examples", "testdata", "bench", "tests/diag", "tests/testdata/badtests", "tests/testdata/doc", "tests/testdata/testpkg", "tests/testdata/vet"} {
		n := len(files)
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".p") && !strings.Contains(path, "invalid") {
				files = append(files, path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == n {
			t.Errorf("no .p files under %s", dir)
		}
	}
	_, errOut, code := runFmt(t, "", append([]string{"-l"}, files...)...)
	if code == 2 || errOut != "" {
		t.Errorf("pluto fmt -l: status %d\n%s", code, errOut)
	}
}
