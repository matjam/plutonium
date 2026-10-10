package diag

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// headingForm matches a numbered heading of the spec: "## 12. Build
	// modes and targets" or "### 12.4 `pluto/arch/x86`".
	headingForm = regexp.MustCompile(`^#{2,6} (\d+(?:\.\d+)*)\.? (.+)$`)
	// pspecForm matches what pspec produces.
	pspecForm = regexp.MustCompile(`^"(.+)" in the Plutonium spec, section (\d+(?:\.\d+)*)$`)
)

// specHeadings returns the numbered headings of docs/spec/file, as a map
// from section number to title with the backquotes removed.
func specHeadings(t *testing.T, file string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec", file))
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]string{}
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingForm.FindStringSubmatch(line); m != nil {
			headings[m[1]] = strings.ReplaceAll(strings.TrimSpace(m[2]), "`", "")
		}
	}
	return headings
}

// TestSpecReferences checks that every reference to the Plutonium spec in
// the catalog names a section that exists in docs/spec/language.md, under
// the title the reference gives. A renumbering of the spec fails here, so
// the references are updated with it.
func TestSpecReferences(t *testing.T) {
	headings := specHeadings(t, "language.md")
	if len(headings) < 20 {
		t.Fatalf("found %d numbered headings in docs/spec/language.md; is the heading form parsed correctly?", len(headings))
	}
	checked := 0
	for _, code := range Codes() {
		e := Lookup(code)
		if e.Spec == "" {
			continue
		}
		m := pspecForm.FindStringSubmatch(e.Spec)
		if m == nil {
			// The other forms cite the Go spec, quote it, or point at the
			// whole of the Plutonium spec. Any other mention of a section
			// of the Plutonium spec must be in pspec's form.
			if strings.Contains(e.Spec, "Plutonium spec") && strings.Contains(e.Spec, "section") {
				t.Errorf("%s (%s): spec reference %q is not in the form pspec produces", code, e.Name, e.Spec)
			}
			continue
		}
		checked++
		title, section := m[1], m[2]
		got, ok := headings[section]
		switch {
		case !ok:
			t.Errorf("%s (%s): docs/spec/language.md has no section %s (cited as %q)", code, e.Name, section, title)
		case got != title:
			t.Errorf("%s (%s): section %s of docs/spec/language.md is %q, not %q", code, e.Name, section, got, title)
		}
	}
	if checked == 0 {
		t.Error("no catalog entry cites a section of the Plutonium spec")
	}
}
