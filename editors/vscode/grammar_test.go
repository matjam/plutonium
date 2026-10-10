// Package vscode holds the tests of the Plutonium extension for VS Code
// (editors/vscode). They keep the grammar in step with the compiler: every
// keyword of internal/syntax/token and every predeclared name of
// internal/types2 must be highlighted. The checks that need a JavaScript or
// Oniguruma regex engine run test/grammar-check.js and test/format-check.js
// with node, and are skipped when node is not installed.
package vscode

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

const grammarFile = "syntaxes/plutonium.tmLanguage.json"

// removed lists the predeclared names that types2 still defines but that
// Plutonium rejects (language spec section 3); the grammar must not
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

func TestKeywords(t *testing.T) {
	kws := words(t, "keywords")
	n := 0
	for tok := token.Token(0); tok < 1000; tok++ {
		if !tok.IsKeyword() {
			continue
		}
		n++
		kw := tok.String()
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
	if n < 27 {
		t.Errorf("found only %d keywords in internal/syntax/token", n)
	}

	// Words that are keywords only in context are matched by other rules;
	// check that the grammar mentions each.
	data, err := os.ReadFile(grammarFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"bits", "owner", "union", "le", "be"} {
		if !regexp.MustCompile(`[\\(|]` + w + `[\\)|]`).Match(data) {
			t.Errorf("contextual keyword %q does not appear in %s", w, grammarFile)
		}
	}
}

func TestPredeclared(t *testing.T) {
	funcs := words(t, "builtin-functions")
	types := words(t, "builtin-types")
	consts := words(t, "builtin-constants")
	ctors := words(t, "type-constructors")

	for _, name := range types2.Universe.Names() {
		var set map[string]string
		var what string
		switch types2.Universe.Lookup(name).(type) {
		case *types2.Builtin:
			set, what = funcs, "builtin-functions"
		case *types2.TypeName:
			set, what = types, "builtin-types"
			if _, ok := ctors[name]; ok {
				continue // volatile, owned and the other type constructors
			}
		case *types2.Const, *types2.Nil:
			set, what = consts, "builtin-constants"
		default:
			continue
		}
		_, ok := set[name]
		if slices.Contains(removed, name) {
			if ok {
				t.Errorf("%s is removed from Plutonium but %s highlights it", name, what)
			}
		} else if !ok {
			t.Errorf("predeclared %s is missing from %s", name, what)
		}
	}
	for _, name := range plutoniumBuiltins {
		if _, ok := funcs[name]; !ok {
			t.Errorf("builtin %s is missing from builtin-functions", name)
		}
	}
	for k := types2.WrapVolatile; k <= types2.WrapSynced; k++ {
		if _, ok := ctors[k.String()]; !ok {
			t.Errorf("type constructor %s is missing from type-constructors", k)
		}
	}
	if _, ok := ctors["weak"]; !ok {
		t.Error("type constructor weak is missing from type-constructors")
	}
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

// TestFormatter runs the extension's formatting provider against a freshly
// built pluto.
func TestFormatter(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	pluto := filepath.Join(t.TempDir(), "pluto")
	build := exec.Command(goBin, "build", "-o", pluto, "github.com/matjam/plutonium/cmd/pluto")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building pluto: %v\n%s", err, out)
	}
	if code := node(t, "test/format-check.js", pluto); code != 0 {
		t.Errorf("format-check.js: exit status %d", code)
	}
}
