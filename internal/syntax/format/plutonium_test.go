package format

import (
	"bytes"
	"flag"
	"fmt"
	"go/constant"
	goformat "go/format"
	gotoken "go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	textdiff "github.com/matjam/plutonium/internal/diff"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the formatter's output")

// repoRoot is the repository root, relative to this package.
const repoRoot = "../../.."

// TestGolden formats each testdata/*.input file and compares the result
// with the .golden file next to it. The golden files cover every
// Plutonium-specific construct; each must also be a fixed point of the
// formatter and parse to the same AST as its input.
func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*.input")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 {
		t.Fatal("no testdata/*.input files")
	}
	for _, input := range inputs {
		t.Run(filepath.Base(input), func(t *testing.T) {
			src, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := File(input, src, false)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(input, ".input") + ".golden"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output differs from %s:\n%s", golden, textdiff.Diff("got", got, "want", want))
			}
			checkFixedPoint(t, golden, want)
			if err := sameAST(input, src, want); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestIdempotent formats every Plutonium source file in the repository:
// formatting twice must give the same result as formatting once, and the
// formatted file must parse to the same AST as the original.
func TestIdempotent(t *testing.T) {
	files := repoSourceFiles(t)
	if len(files) < 50 {
		t.Fatalf("found only %d .p files; is the repository layout different?", len(files))
	}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		once, err := File(file, src, false)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		checkFixedPoint(t, file, once)
		if err := sameAST(file, src, once); err != nil {
			t.Error(err)
		}
	}
}

// TestSameAsGoFormat checks that plain Go formats exactly as go/format
// formats it. The corpus is Go's own go/... sources and printer test data,
// this repository's Go files, and every .p file that go/parser accepts.
func TestSameAsGoFormat(t *testing.T) {
	var files []string
	if goroot := goRoot(); goroot != "" {
		files = append(files, goFiles(t, filepath.Join(goroot, "src", "go"), ".go")...)
		files = append(files, goFiles(t, filepath.Join(goroot, "src", "go", "printer", "testdata"), ".input", ".golden")...)
		files = append(files, goFiles(t, filepath.Join(goroot, "src", "cmd", "gofmt", "testdata"), ".input", ".golden")...)
	} else {
		t.Log("go env GOROOT failed; checking only this repository's files")
	}
	files = append(files, goFiles(t, filepath.Join(repoRoot, "internal"), ".go")...)
	files = append(files, goFiles(t, filepath.Join(repoRoot, "cmd"), ".go")...)
	files = append(files, repoSourceFiles(t)...)

	compared := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := goformat.Source(src)
		if err != nil {
			continue // not Go, or a deliberately broken test input
		}
		got, err := Source(src)
		if err != nil {
			// The only Go that Plutonium does not parse uses its
			// keyword "packed" as an identifier.
			if !bytes.Contains(src, []byte("packed")) {
				t.Errorf("%s: go/format accepts it but format.Source fails: %v", file, err)
			}
			continue
		}
		compared++
		if !bytes.Equal(got, want) {
			t.Errorf("%s: output differs from go/format:\n%s", file, textdiff.Diff("go/format", want, "format", got))
		}
	}
	if compared < 100 {
		t.Errorf("compared only %d files", compared)
	}
	t.Logf("compared %d files with go/format", compared)
}

// TestFileErrors checks that syntax errors carry the file name, and that a
// fragment is accepted only when asked for.
func TestFileErrors(t *testing.T) {
	_, err := File("bad.p", []byte("package p\nfunc {"), false)
	if err == nil || !strings.HasPrefix(err.Error(), "bad.p:2:") {
		t.Errorf("error = %v, want one at bad.p:2", err)
	}
	frag := []byte("x  :=  packed uint8 { a bits[uint8,8] }{}\n")
	// (A literal of an instantiated type constructor, ring[int, 8]{},
	// does not parse in expression position yet, so it is not used here.)
	if _, err := File("frag.p", frag, false); err == nil {
		t.Error("statement list accepted as a file")
	}
	got, err := File("frag.p", frag, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "x := packed uint8 { a bits[uint8, 8] }{}\n"; string(got) != want {
		t.Errorf("fragment formats as %q, want %q", got, want)
	}
}

func checkFixedPoint(t *testing.T, name string, src []byte) {
	t.Helper()
	again, err := File(name, src, false)
	if err != nil {
		t.Errorf("%s: formatted output does not parse: %v", name, err)
		return
	}
	if !bytes.Equal(again, src) {
		t.Errorf("%s: formatting is not idempotent:\n%s", name, textdiff.Diff("once", src, "twice", again))
	}
}

// sameAST parses two versions of a file and reports the first difference
// between their syntax trees, ignoring positions and comments. Imports are
// sorted first, since the formatter sorts them.
func sameAST(name string, a, b []byte) error {
	fsetA := token.NewFileSet()
	fa, err := parser.ParseFile(fsetA, name, a, parser.ParseComments)
	if err != nil {
		return err
	}
	ast.SortImports(fsetA, fa)
	fb, err := parser.ParseFile(token.NewFileSet(), name, b, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("%s: formatted output does not parse: %v", name, err)
	}
	if err := equalNodes(reflect.ValueOf(fa), reflect.ValueOf(fb), "File"); err != nil {
		return fmt.Errorf("%s: formatting changed the syntax tree: %v", name, err)
	}
	return nil
}

var (
	posType     = reflect.TypeFor[token.Pos]()
	commentType = reflect.TypeFor[*ast.CommentGroup]()
	commentList = reflect.TypeFor[[]*ast.CommentGroup]()
	basicLit    = reflect.TypeFor[ast.BasicLit]()
)

func equalNodes(a, b reflect.Value, path string) error {
	if a.Type() != b.Type() {
		return fmt.Errorf("%s: %s vs %s", path, a.Type(), b.Type())
	}
	switch a.Type() {
	case posType, commentType, commentList:
		return nil
	case basicLit:
		return equalLits(a.Addr().Interface().(*ast.BasicLit), b.Addr().Interface().(*ast.BasicLit), path)
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Errorf("%s: nil vs non-nil", path)
			}
			return nil
		}
		return equalNodes(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		for i := range a.NumField() {
			f := a.Type().Field(i)
			if f.Name == "FileStart" || f.Name == "FileEnd" {
				continue
			}
			if err := equalNodes(a.Field(i), b.Field(i), path+"."+f.Name); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice:
		if a.Len() != b.Len() {
			return fmt.Errorf("%s: %d vs %d elements", path, a.Len(), b.Len())
		}
		for i := range a.Len() {
			if err := equalNodes(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.String:
		if a.String() != b.String() {
			return fmt.Errorf("%s: %q vs %q", path, a.String(), b.String())
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			return fmt.Errorf("%s: %d vs %d", path, a.Int(), b.Int())
		}
		return nil
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			return fmt.Errorf("%s: %t vs %t", path, a.Bool(), b.Bool())
		}
		return nil
	}
	return fmt.Errorf("%s: unhandled kind %s", path, a.Kind())
}

// equalLits compares literals by value, since the formatter normalises the
// spelling of numbers (0X1F becomes 0x1f).
func equalLits(a, b *ast.BasicLit, path string) error {
	if a.Kind != b.Kind {
		return fmt.Errorf("%s: literal kinds %s vs %s", path, a.Kind, b.Kind)
	}
	if a.Value == b.Value {
		return nil
	}
	switch a.Kind {
	case token.INT, token.FLOAT, token.IMAG:
		va := constant.MakeFromLiteral(a.Value, gotoken.Token(a.Kind), 0)
		vb := constant.MakeFromLiteral(b.Value, gotoken.Token(b.Kind), 0)
		if constant.Compare(va, gotoken.EQL, vb) {
			return nil
		}
	}
	return fmt.Errorf("%s: literal %s vs %s", path, a.Value, b.Value)
}

// repoSourceFiles lists the .p files of lib, examples, testdata and the
// Plutonium versions of the benchmarks.
func repoSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"lib", "examples", "testdata"} {
		files = append(files, goFiles(t, filepath.Join(repoRoot, dir), ".p")...)
	}
	bench, err := filepath.Glob(filepath.Join(repoRoot, "bench", "*", "pu"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range bench {
		files = append(files, goFiles(t, dir, ".p")...)
	}
	return files
}

// goFiles lists the files under dir with one of the given suffixes.
func goFiles(t *testing.T, dir string, suffixes ...string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, s := range suffixes {
			if !d.IsDir() && strings.HasSuffix(path, s) {
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
