package diag

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// checkerCodes returns the codes declared in internal/types2/errors, by
// name, with their values (found through the generated String method).
func checkerCodes(t *testing.T) map[string]typerr.Code {
	t.Helper()
	byName := map[string]typerr.Code{}
	for v := -1; v < 3000; v++ {
		if s := typerr.Code(v).String(); !strings.HasPrefix(s, "Code(") {
			byName[s] = typerr.Code(v)
		}
	}
	codes := map[string]typerr.Code{}
	for _, file := range []string{"codes.go", "plutonium.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "types2", "errors", file), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, s := range gd.Specs {
				for _, id := range s.(*ast.ValueSpec).Names {
					if id.Name == "_" || id.Name == "Test" {
						continue
					}
					v, ok := byName[id.Name]
					if !ok {
						t.Errorf("%s: code %s has no String: run go generate in internal/types2/errors", file, id.Name)
						continue
					}
					codes[id.Name] = v
				}
			}
		}
	}
	return codes
}

// diagCodes returns the codes declared in this package's codes.go.
func diagCodes(t *testing.T) map[string]Code {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "codes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]Code{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, id := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("code %s is not a string literal", id.Name)
				}
				v, _ := strconv.Unquote(lit.Value)
				codes[id.Name] = Code(v)
			}
		}
	}
	return codes
}

// TestCatalogComplete checks that every error code, of the type checker
// and of the rest of the compiler, has an explanation, and that pluto
// explain can print it.
func TestCatalogComplete(t *testing.T) {
	used := map[Code]string{}
	for name, c := range checkerCodes(t) {
		if c == typerr.InvalidSyntaxTree {
			continue // reported as InternalError
		}
		code := CheckerCode(c)
		used[code] = name
		e := Lookup(code)
		if e == nil {
			t.Errorf("checker code %s (%s) has no catalog entry", name, code)
			continue
		}
		if e.Name != name {
			t.Errorf("%s: catalog names it %s, the checker %s", code, e.Name, name)
		}
	}
	for name, code := range diagCodes(t) {
		if other, dup := used[code]; dup {
			t.Errorf("%s is used by both %s and %s", code, other, name)
		}
		used[code] = name
		if e := Lookup(code); e == nil {
			t.Errorf("code %s (%s) has no catalog entry", name, code)
		} else if e.Name != name {
			t.Errorf("%s: catalog names it %s, codes.go %s", code, e.Name, name)
		}
	}
	for _, code := range Codes() {
		if _, ok := used[code]; !ok {
			t.Errorf("catalog entry %s has no code", code)
		}
		if _, err := Explain(code); err != nil {
			t.Errorf("Explain(%s): %v", code, err)
		}
	}
}

var codeForm = regexp.MustCompile(`^E\d{4}$`)

// TestCatalogStyle checks the catalog's entries against the rules for
// explanations: a code in its range, text for every entry, a reason and a
// spec section for every Plutonium rule, and plain punctuation.
func TestCatalogStyle(t *testing.T) {
	for _, code := range Codes() {
		e := Lookup(code)
		if !codeForm.MatchString(string(code)) {
			t.Errorf("%s: malformed code", code)
		}
		if e.Text == "" || e.Name == "" {
			t.Errorf("%s: no name or text", code)
		}
		n, _ := strconv.Atoi(string(code[1:]))
		if n >= 2000 && n < 3000 && e.Spec == "" {
			t.Errorf("%s (%s): a Plutonium rule needs a spec section", code, e.Name)
		}
		all := e.Text + e.Why + e.Spec + e.Example + e.Fixed
		if e.Fix != nil {
			all += e.Fix.Intro + e.Fix.Code
		}
		for _, bad := range []string{"\u2014", "\u2013", "TODO"} {
			if strings.Contains(all, bad) {
				t.Errorf("%s (%s): contains %q", code, e.Name, bad)
			}
		}
	}
	// The rules that differ from Go say why.
	for _, c := range []Code{CheckerCode(typerr.MapNotPermitted), CheckerCode(typerr.ErrorsNewNotConstant),
		CheckerCode(typerr.MissingEnumCase), KernelFloat, DeferInLoop, StringConcat, AllocatingConv} {
		if Lookup(c).Why == "" {
			t.Errorf("%s: no reason why Plutonium differs from Go", c)
		}
	}
}

// TestGoDocs checks that the generated catalog matches the type checker's
// codes and is up to date with internal/types2/errors/codes.go.
func TestGoDocs(t *testing.T) {
	for _, d := range goDocs {
		if got := typerr.Code(d.Value).String(); got != d.Name {
			t.Errorf("goDocs has %s = %d, the checker %s", d.Name, d.Value, got)
		}
	}
	if testing.Short() {
		return
	}
	out := filepath.Join(t.TempDir(), "catalog_go.go")
	cmd := exec.Command("go", "run", "./gencatalog", "-out", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gencatalog: %v\n%s", err, msg)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("catalog_go.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("catalog_go.go is out of date: run go generate in internal/diag")
	}
}

// TestExplain checks the long explanation's form.
func TestExplain(t *testing.T) {
	text, err := Explain("e2001")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"E2001: MapNotPermitted\n\nPlutonium has no built-in map type.",
		"\nFor example:\n\n    counts := map[string]int{}\n",
		"\nWritten correctly:\n\n    counts := hash.NewMap[string, int](0)\n",
		"\nSee \"What Plutonium removes from Go\" in the Plutonium spec, section 3.\n",
	}
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("explanation lacks %q:\n%s", w, text)
		}
	}
	if text, _ := Explain(BadCSignature); !strings.Contains(text, "\nPass a pointer and a length:\n\n    //pu:extern write\n") {
		t.Errorf("explanation without Fixed lacks the fix:\n%s", text)
	}
	if _, err := Explain("E0000"); err == nil {
		t.Error("Explain(E0000) succeeded")
	}
}
