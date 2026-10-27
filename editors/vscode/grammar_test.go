// Package vscode holds the tests of the Plutonium extension for VS Code
// (editors/vscode). They keep the grammar in step with the compiler: every
// keyword of compiler/internal/syntax/token and every predeclared name of
// compiler/internal/types2 must be highlighted. The checks that need a JavaScript or
// Oniguruma regex engine run test/grammar-check.js and test/format-check.js
// with node, and are skipped when node is not installed.
package vscode

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	gotypes "go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const grammarFile = "syntaxes/plutonium.tmLanguage.json"

// removed lists the predeclared names that the tables of types2 still list
// but that Plutonium rejects (language spec section 3); the grammar must not
// highlight them as builtins.
var removed = []string{"complex", "complex64", "complex128", "real", "imag", "delete", "recover", "append"}

// plutoniumBuiltins lists predeclared functions the grammar must highlight
// even before the checker defines them.
var plutoniumBuiltins = []string{"take"}

func readJSON(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func TestManifest(t *testing.T) {
	pkg := readJSON(t, "package.json")
	for key, want := range map[string]string{
		"name":        "plutonium",
		"publisher":   "matjam",
		"displayName": "Plutonium",
		"main":        "./extension.js",
	} {
		if got, _ := pkg[key].(string); got != want {
			t.Errorf("package.json %s = %q, want %q", key, got, want)
		}
	}
	if _, err := os.Stat(pkg["main"].(string)); err != nil {
		t.Errorf("package.json main: %v", err)
	}
	contributes := pkg["contributes"].(map[string]any)

	langs := contributes["languages"].([]any)
	lang := langs[0].(map[string]any)
	if lang["id"] != "plutonium" {
		t.Errorf("language id = %v, want plutonium", lang["id"])
	}
	if exts, _ := lang["extensions"].([]any); !slices.Contains(exts, any(".p")) {
		t.Errorf("language extensions = %v, want .p", exts)
	}
	readJSON(t, lang["configuration"].(string))

	gram := contributes["grammars"].([]any)[0].(map[string]any)
	if gram["language"] != "plutonium" {
		t.Errorf("grammar language = %v, want plutonium", gram["language"])
	}
	g := readJSON(t, gram["path"].(string))
	if g["scopeName"] != gram["scopeName"] {
		t.Errorf("grammar scopeName %v does not match package.json's %v", g["scopeName"], gram["scopeName"])
	}
}

// wordListRE matches a pattern of the form \b(?:a|b|c)\b, optionally
// followed by a lookahead.
var wordListRE = regexp.MustCompile(`^\\b\(\?:([A-Za-z0-9_|]+)\)\\b`)

// words returns the words matched by the word-list patterns of a repository
// entry of the grammar, mapped to their scope names.
func words(t *testing.T, key string) map[string]string {
	t.Helper()
	repo := readJSON(t, grammarFile)["repository"].(map[string]any)
	entry, ok := repo[key].(map[string]any)
	if !ok {
		t.Fatalf("grammar has no repository entry %q", key)
	}
	rules := []any{entry}
	if p, ok := entry["patterns"].([]any); ok {
		rules = p
	}
	out := map[string]string{}
	for _, r := range rules {
		rule := r.(map[string]any)
		match, _ := rule["match"].(string)
		m := wordListRE.FindStringSubmatch(match)
		if m == nil {
			continue
		}
		for w := range strings.SplitSeq(m[1], "|") {
			out[w] = rule["name"].(string)
		}
	}
	if len(out) == 0 {
		t.Fatalf("repository entry %q has no word-list patterns", key)
	}
	return out
}

// sourceFile parses a source file of the self-hosted compiler, named
// relative to compiler/internal. The files read here use only syntax that
// Plutonium shares with Go, so go/parser reads them.
func sourceFile(t *testing.T, name string) *ast.File {
	t.Helper()
	path := filepath.Join("..", "..", "compiler", "internal", filepath.FromSlash(name))
	f, err := parser.ParseFile(gotoken.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// funcDecl returns the function of f named name; with method set, the
// method of that name instead.
func funcDecl(t *testing.T, f *ast.File, name string, method bool) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && (fd.Recv != nil) == method {
			return fd
		}
	}
	t.Fatalf("%s: no function %s", f.Name.Name, name)
	return nil
}

// varElts returns the elements of the composite literal that initializes
// the package-level variable name of f.
func varElts(t *testing.T, f *ast.File, name string) []ast.Expr {
	t.Helper()
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != gotoken.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if n.Name != name || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.CompositeLit); ok {
					return lit.Elts
				}
			}
		}
	}
	t.Fatalf("%s: no variable %s initialized by a composite literal", f.Name.Name, name)
	return nil
}

// str returns the value of e if it is a string literal.
func str(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != gotoken.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// field returns the string literal at index i of the composite literal e,
// or of its value if e is a key-value pair.
func field(t *testing.T, e ast.Expr, i int) string {
	t.Helper()
	if kv, ok := e.(*ast.KeyValueExpr); ok {
		e = kv.Value
	}
	if lit, ok := e.(*ast.CompositeLit); ok && i < len(lit.Elts) {
		if s, ok := str(lit.Elts[i]); ok {
			return s
		}
	}
	t.Fatalf("table entry %s has no string at index %d", gotypes.ExprString(e), i)
	return ""
}

// keyName returns the identifier key of a key-value element.
func keyName(t *testing.T, e ast.Expr) string {
	t.Helper()
	if kv, ok := e.(*ast.KeyValueExpr); ok {
		if id, ok := kv.Key.(*ast.Ident); ok {
			return id.Name
		}
	}
	t.Fatalf("table entry %s has no identifier key", gotypes.ExprString(e))
	return ""
}

// keywords returns the keywords of compiler/internal/syntax/token, as
// Token.IsKeyword defines them: the tokens declared between keyword_beg and
// keyword_end, and the ones it compares against besides. They are spelled
// as in the tokens table.
func keywords(t *testing.T) []string {
	f := sourceFile(t, "syntax/token/token.p")

	isKeyword := funcDecl(t, f, "IsKeyword", true)
	bounds := map[string]bool{}
	var extra []string
	ast.Inspect(isKeyword.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if n.Name == "keyword_beg" || n.Name == "keyword_end" {
				bounds[n.Name] = true
			}
		case *ast.BinaryExpr:
			if y, ok := n.Y.(*ast.Ident); ok && n.Op == gotoken.EQL {
				extra = append(extra, y.Name)
			}
		}
		return true
	})
	if len(bounds) != 2 {
		t.Fatal("Token.IsKeyword no longer tests keyword_beg < tok < keyword_end; update keywords in grammar_test.go")
	}

	var names []string
	in := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != gotoken.CONST {
			continue
		}
		for _, s := range gd.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				switch {
				case n.Name == "keyword_beg":
					in = true
				case n.Name == "keyword_end":
					in = false
				case in:
					names = append(names, n.Name)
				}
			}
		}
	}
	names = append(names, extra...)

	spelling := map[string]string{}
	for _, e := range varElts(t, f, "tokens") {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if s, ok := str(kv.Value); ok {
				spelling[keyName(t, e)] = s
			}
		}
	}
	var kws []string
	for _, n := range names {
		s, ok := spelling[n]
		if !ok {
			t.Fatalf("keyword token %s has no spelling in the tokens table", n)
		}
		kws = append(kws, s)
	}
	return kws
}

func TestKeywords(t *testing.T) {
	kws := words(t, "keywords")
	all := keywords(t)
	for _, kw := range all {
		scope, ok := kws[kw]
		switch {
		case !ok:
			t.Errorf("keyword %q is not highlighted: add it to the keywords patterns of %s", kw, grammarFile)
		case kw == "map":
			if !strings.HasPrefix(scope, "invalid.deprecated.") {
				t.Errorf("map has scope %s; it is removed from Plutonium and should be invalid.deprecated", scope)
			}
		case !strings.HasPrefix(scope, "keyword."):
			t.Errorf("keyword %q has scope %s, want keyword.*", kw, scope)
		}
	}
	if len(all) < 27 {
		t.Errorf("found only %d keywords in compiler/internal/syntax/token", len(all))
	}

	// Words that are keywords only in context are matched by other rules;
	// check that the grammar mentions each.
	data, err := os.ReadFile(grammarFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"bits", "owner", "union", "task", "or", "le", "be"} {
		if !regexp.MustCompile(`[\\(|]` + w + `[\\)|]`).Match(data) {
			t.Errorf("contextual keyword %q does not appear in %s", w, grammarFile)
		}
	}
}

// universe lists the predeclared names of compiler/internal/types2, read
// from the tables its universe scope is built from.
type universe struct {
	types, consts, funcs, ctors []string
	// undeclared lists the builtins in predeclaredFuncs that
	// defPredeclaredFuncs leaves out of the universe for Plutonium.
	undeclared []string
}

func readUniverse(t *testing.T) universe {
	f := sourceFile(t, "types2/universe.p")
	var u universe

	// Basic types, their aliases, and error, any, comparable and portable.
	// Like def, leave out the internal names (with blanks) and the exported
	// ones, which go into package unsafe.
	for _, e := range varElts(t, f, "Typ") {
		if name := field(t, e, 2); !strings.Contains(name, " ") && !gotoken.IsExported(name) {
			u.types = append(u.types, name)
		}
	}
	for _, e := range varElts(t, f, "basicAliases") {
		u.types = append(u.types, field(t, e, 2))
	}
	ast.Inspect(funcDecl(t, f, "defPredeclaredTypes", false).Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) >= 3 {
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "NewTypeName" {
				if s, ok := str(call.Args[2]); ok {
					u.types = append(u.types, s)
				}
			}
		}
		return true
	})

	for _, e := range varElts(t, f, "predeclaredConsts") {
		u.consts = append(u.consts, field(t, e, 0))
	}
	ast.Inspect(funcDecl(t, f, "defPredeclaredNil", false).Body, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "name" {
				if s, ok := str(kv.Value); ok {
					u.consts = append(u.consts, s)
				}
			}
		}
		return true
	})

	// defPredeclaredFuncs skips the testing builtins (id == _Assert) and
	// the ones Plutonium removes (a switch case); exported names go into
	// package unsafe.
	skipped, undeclared := map[string]bool{}, map[string]bool{}
	ast.Inspect(funcDecl(t, f, "defPredeclaredFuncs", false).Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if x, ok := n.X.(*ast.Ident); ok && x.Name == "id" && n.Op == gotoken.EQL {
				if y, ok := n.Y.(*ast.Ident); ok {
					skipped[y.Name] = true
				}
			}
		case *ast.CaseClause:
			for _, e := range n.List {
				if id, ok := e.(*ast.Ident); ok {
					undeclared[id.Name] = true
				}
			}
		}
		return true
	})
	if len(skipped) == 0 || len(undeclared) == 0 {
		t.Fatal("defPredeclaredFuncs no longer has the shape grammar_test.go reads; update readUniverse")
	}
	for _, e := range varElts(t, f, "predeclaredFuncs") {
		id, name := keyName(t, e), field(t, e, 0)
		switch {
		case skipped[id] || gotoken.IsExported(name):
		case undeclared[id]:
			u.undeclared = append(u.undeclared, name)
		default:
			u.funcs = append(u.funcs, name)
		}
	}

	ctors := sourceFile(t, "types2/plutonium_types.p")
	for _, e := range varElts(t, ctors, "wrapperNames") {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("wrapperNames entry %s has no key", gotypes.ExprString(e))
		}
		s, ok := str(kv.Value)
		if !ok {
			t.Fatalf("wrapperNames entry %s is not a string", gotypes.ExprString(e))
		}
		u.ctors = append(u.ctors, s)
	}

	if len(u.types) < 20 || len(u.consts) < 4 || len(u.funcs) < 12 || len(u.ctors) < 7 {
		t.Fatalf("read too few predeclared names from compiler/internal/types2: %+v", u)
	}
	return u
}

func TestPredeclared(t *testing.T) {
	u := readUniverse(t)
	check := func(names []string, key string) {
		set := words(t, key)
		for _, name := range names {
			_, ok := set[name]
			if slices.Contains(removed, name) {
				if ok {
					t.Errorf("%s is removed from Plutonium but %s highlights it", name, key)
				}
			} else if !ok {
				t.Errorf("predeclared %s is missing from %s", name, key)
			}
		}
	}
	check(u.types, "builtin-types")
	check(u.consts, "builtin-constants")
	check(u.funcs, "builtin-functions")

	funcs := words(t, "builtin-functions")
	for _, name := range u.undeclared {
		if !slices.Contains(removed, name) {
			t.Errorf("builtin %s is left out of the universe but not listed in removed", name)
		}
		if _, ok := funcs[name]; ok {
			t.Errorf("%s is removed from Plutonium but builtin-functions highlights it", name)
		}
	}
	for _, name := range plutoniumBuiltins {
		if _, ok := funcs[name]; !ok {
			t.Errorf("builtin %s is missing from builtin-functions", name)
		}
	}
	ctors := words(t, "type-constructors")
	for _, name := range u.ctors {
		if _, ok := ctors[name]; !ok {
			t.Errorf("type constructor %s is missing from type-constructors", name)
		}
	}
}

// plutoBinary returns the pluto the tests run, with the repository's
// library as $PLUTOROOT: $PLUTO_TEST_COMPILER if set, otherwise pluto on
// PATH. It skips the test when there is neither.
func plutoBinary(t *testing.T) string {
	t.Helper()
	pluto := os.Getenv("PLUTO_TEST_COMPILER")
	if pluto == "" {
		var err error
		if pluto, err = exec.LookPath("pluto"); err != nil {
			t.Skip("no pluto compiler: set PLUTO_TEST_COMPILER or put pluto on PATH")
		}
	}
	pluto, err := filepath.Abs(pluto)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLUTOROOT", lib)
	return pluto
}

// node runs a script under test/ with node and returns its exit status,
// skipping the test when node is not installed.
func node(t *testing.T, args ...string) int {
	t.Helper()
	bin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	t.Logf("node %s:\n%s", strings.Join(args, " "), out)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

// TestRegexps compiles every regex in the grammar with JavaScript's RegExp
// (u flag), which rejects the constructs only Oniguruma supports.
func TestRegexps(t *testing.T) {
	if code := node(t, "test/grammar-check.js", "regexps"); code != 0 {
		t.Errorf("grammar-check.js regexps: exit status %d", code)
	}
}

// TestTokens checks the scope assertions in test/sample.p with the
// vscode-textmate and Oniguruma modules of an installed editor, and that
// every .p file in the repository ends with no grammar region open.
func TestTokens(t *testing.T) {
	switch code := node(t, "test/grammar-check.js", "tokens"); code {
	case 0:
	case 3:
		t.Skip("no editor installation with vscode-textmate found")
	default:
		t.Errorf("grammar-check.js tokens: exit status %d", code)
	}
}

// TestFormatter runs the extension's formatting provider against pluto
// (see plutoBinary).
func TestFormatter(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	pluto := plutoBinary(t)
	if code := node(t, "test/format-check.js", pluto); code != 0 {
		t.Errorf("format-check.js: exit status %d", code)
	}
}
