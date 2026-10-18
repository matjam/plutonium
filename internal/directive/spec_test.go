package directive

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/syntax/token"
)

// specDirectives returns the directives that the tables of section 20.3 of
// the specification list, by name, and the forms written with an argument
// that the table lists as rows of their own (such as `//pu:interrupt trap`).
func specDirectives(t *testing.T) (names, forms []string) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", "language", "20-system-considerations.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	i := strings.Index(s, "### 20.3 Directives")
	j := strings.Index(s, "### 20.4 ")
	if i < 0 || j < i {
		t.Fatal("section 20.3 not found in the specification")
	}
	row := regexp.MustCompile("(?m)^\\| `//pu:([a-z]+)([^`]*)`")
	seen := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(s[i:j], -1) {
		if m[2] == " trap" {
			forms = append(forms, m[1]+m[2])
		}
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	slices.Sort(names)
	return names, forms
}

// TestSpecDirectives is the directive audit: the directives the compiler
// accepts are exactly the ones the tables of section 20.3 list, and the form
// of //pu:interrupt that the specification lists is the one the compiler takes.
func TestSpecDirectives(t *testing.T) {
	names, forms := specDirectives(t)
	// build is written before the package clause and has its own table row.
	if got := Names(); !slices.Equal(got, names) {
		t.Errorf("the compiler's directives differ from section 20.3\ncompiler: %v\nspec:     %v", got, names)
	}
	if want := []string{"interrupt trap"}; !slices.Equal(forms, want) {
		t.Errorf("forms of directives in section 20.3: %v, want %v", forms, want)
	}
	// Every retired directive is named in the specification as retired, and
	// none is also a directive.
	text, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", "language", "20-system-considerations.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range Retired() {
		if slices.Contains(names, n) {
			t.Errorf("%s is both listed and retired", n)
		}
		if !strings.Contains(string(text), n) {
			t.Errorf("the specification does not mention the retired directive %s", n)
		}
	}
	// The argument of //pu:interrupt: the only one the compiler takes.
	for _, c := range []struct {
		src  string
		want bool
	}{
		{"//pu:interrupt", true},
		{"//pu:interrupt trap", true},
		{"//pu:interrupt fast", false},
		{"//pu:interrupt trap fast", false},
	} {
		name, arg, ok := Split(c.src)
		if !ok {
			t.Fatalf("%q is not a directive", c.src)
		}
		got := argCheck(token.Position{}, name, arg, directives[name]) == nil
		if got != c.want {
			t.Errorf("%q accepted: %v, want %v", c.src, got, c.want)
		}
	}
}
