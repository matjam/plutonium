package directive

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/parser"
	"github.com/matjam/plutonium/internal/syntax/token"
)

func check(t *testing.T, src string) []*diag.Diagnostic {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.p", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Check(fset, f)
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // "code: message", in order
	}{
		{"fine", "//pu:build amd64\n\npackage p\n\n//pu:noinline\nfunc f() {}\n\n//pu:extern strlen\nfunc g()\n\n//pu:export\nfunc h() {}\n\n//pu:percpu\nvar v int\n\nvar (\n\t//pu:scheduler\n\ts int\n\t//pu:section .data\n\tw int\n)\n\n//pu:stack 1M\nfunc main() {}\n", nil},
		{"typo", "package p\n\n//pu:interupt\nfunc f() {}\n", []string{"E2600: unknown directive //pu:interupt (did you mean //pu:interrupt?)"}},
		{"far", "package p\n\n//pu:frobnicate\nfunc f() {}\n", []string{"E2600: unknown directive //pu:frobnicate"}},
		{"empty", "package p\n\n//pu:\nfunc f() {}\n", []string{"E2600: //pu: names no directive"}},
		{"alloc", "package p\n\n//pu:alloc\nfunc f() {}\n", []string{"E2601: //pu:alloc was removed: kernel code may allocate strings anywhere the allocator may be called"}},
		{"bytewise", "package p\n\n//pu:bytewise\nfunc f() {}\n", []string{"E2601: //pu:bytewise was removed: the compiler proves that string slice bounds fall on rune boundaries"}},
		{"gofunc", "package p\n\n//pu:gofunc\nfunc f() {}\n", []string{"E2601: //pu:gofunc was removed: the compiler finds which functions start puroutines with their func arguments"}},
		{"schedhook", "package p\n\n//pu:schedhook\nfunc f()\n\n//pu:schedlate\nfunc g()\n", []string{
			"E2601: //pu:schedhook was removed: the compiler knows the runtime's scheduler hooks by name",
			"E2601: //pu:schedlate was removed: the compiler knows the runtime's scheduler hooks by name"}},
		{"noreturn body", "package p\n\n//pu:noreturn\nfunc f() {\n\tfor {\n\t}\n}\n", []string{"E2601: //pu:noreturn is inferred for a function with a body: delete it"}},
		{"noreturn bodyless", "package p\n\n//pu:noreturn\nfunc f()\n", nil},
		{"noreturn on var", "package p\n\n//pu:noreturn\nvar v int\n", []string{"E2602: //pu:noreturn is in the wrong place: it goes in the doc comment of a function, not of a package-level variable"}},
		{"percpu on func", "package p\n\n//pu:percpu\nfunc f() {}\n", []string{"E2602: //pu:percpu is in the wrong place: it goes in the doc comment of a package-level variable, not of a function"}},
		{"inline on var", "package p\n\n//pu:inline\nvar v int\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a package-level variable"}},
		{"inline on type", "package p\n\n//pu:inline\ntype T int\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a type declaration"}},
		{"inline on grouped type", "package p\n\ntype (\n\t//pu:inline\n\tT int\n)\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a type declaration"}},
		{"inline on const", "package p\n\n//pu:inline\nconst c = 1\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of a const declaration"}},
		{"inline on import", "package p\n\n//pu:inline\nimport \"unsafe\"\n", []string{"E2602: //pu:inline is in the wrong place: it goes in the doc comment of a function, not of an import declaration"}},
		{"in body", "package p\n\nfunc f() {\n\t//pu:noinline\n\tf()\n}\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function, directly above it; it is not above any declaration here"}},
		{"floating", "package p\n\n//pu:noinline\n\nfunc f() {}\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function, directly above it; it is not above any declaration here"}},
		{"build late", "package p\n\n//pu:build amd64\nfunc f() {}\n", []string{"E2602: //pu:build is in the wrong place: it goes before the package clause, where it chooses whether the file is part of the build; here it is ignored"}},
		{"noinline before package", "//pu:noinline\n\npackage p\n", []string{"E2602: //pu:noinline is in the wrong place: it goes in the doc comment of a function; it is before the package clause here"}},
		{"arg given", "package p\n\n//pu:inline hot\nfunc f() {}\n", []string{`E2603: //pu:inline takes no argument, found "hot"`}},
		{"arg missing", "package p\n\n//pu:extern\nfunc f()\n\n//pu:section\nvar v int\n", []string{
			"E2603: //pu:extern needs an argument: the name of the C symbol",
			"E2603: //pu:section needs an argument: the name of the section"}},
		{"build no expr", "//pu:build\n\npackage p\n", []string{"E2603: //pu:build needs an argument: a build constraint such as amd64 && !kernel"}},
		{"not a directive", "package p\n\n// pu:noinline\n/* //pu:x */\nfunc f() {}\n", nil},
		{"method", "package p\n\ntype T int\n\n//pu:noinline\nfunc (T) m() {}\n", nil},
		{"section on func", "package p\n\n//pu:section .text.hot\nfunc f() {}\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, d := range check(t, tt.src) {
				got = append(got, string(d.Code)+": "+d.Msg)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestSplit(t *testing.T) {
	for _, tt := range []struct{ text, name, arg string }{
		{"//pu:inline", "inline", ""},
		{"//pu:extern  strlen ", "extern", "strlen"},
		{"//pu:extern\tstrlen", "extern", "strlen"},
		{"//pu:", "", ""},
	} {
		name, arg, ok := Split(tt.text)
		if !ok || name != tt.name || arg != tt.arg {
			t.Errorf("Split(%q) = %q, %q, %v", tt.text, name, arg, ok)
		}
	}
	if _, _, ok := Split("// pu:inline"); ok {
		t.Error("Split of a comment with a space is a directive")
	}
}

func TestNames(t *testing.T) {
	if len(Names()) != len(directives) || len(Retired()) != len(retired) {
		t.Error("Names or Retired disagree with their tables")
	}
	// The catalog lists the valid directives and the removed ones.
	text := diag.Lookup(diag.UnknownDirective).Text
	for _, n := range Names() {
		if !strings.Contains(text, n) {
			t.Errorf("the E2600 catalog text does not list the directive %s", n)
		}
	}
	text = diag.Lookup(diag.RetiredDirective).Text
	for _, n := range Retired() {
		if !strings.Contains(text, "//pu:"+n) {
			t.Errorf("the E2601 catalog text does not list the removed directive %s", n)
		}
	}
}

// TestTree checks every Plutonium source file of the repository, so that a
// retired or misspelt directive left in the library, an example or a
// benchmark fails here. The programs that exist to show these errors are
// left out.
func TestTree(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	n := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if e.IsDir() {
			if e.Name() == ".git" || e.Name() == ".claude" || strings.HasSuffix(slash, "cmd/pluto/testdata/diag") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".p" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil // programs that test syntax errors
		}
		n++
		for _, d := range Check(fset, f) {
			t.Errorf("%v", d)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Errorf("checked only %d files", n)
	}
}
