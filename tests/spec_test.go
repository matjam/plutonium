package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// headingForm matches a numbered heading of the spec: "## 20. System
	// considerations", "### 20.8 `pluto/arch/x86`" or, in an appendix,
	// "### A.2 Removed".
	headingForm = regexp.MustCompile(`^#{2,6} ((?:\d+|[A-Z])(?:\.\d+)*)\.? (.+)$`)
	// pspecForm matches what pspec produces.
	pspecForm = regexp.MustCompile(`^"(.+)" in the Plutonium spec, section ((?:\d+|[A-Z])(?:\.\d+)*)$`)
	// chapterFile matches the name of a chapter file of the spec,
	// "11-expressions.md" or "a-differences-from-go.md", and captures the
	// chapter number or appendix letter.
	chapterFile = regexp.MustCompile(`^(\d\d|[a-z])-[a-z0-9-]+\.md$`)
	// sectionMention matches a section number mentioned in prose: "section
	// 18.3", "(language spec section 7.9)".
	sectionMention = regexp.MustCompile(`\bsection ((?:\d+|[A-Z])(?:\.\d+)*)\b`)
)

// specDir is the directory that holds the chapters of the spec.
var specDir = filepath.Join("..", "docs", "spec", "language")

// pendingDecisions lists the open questions that the spec states in an
// `> **Open (Gn).**` block because they wait for a decision of the user. Any
// other open block fails TestSpecReferences. Remove an entry, and its block,
// when the decision is recorded.
var pendingDecisions = []string{}

// pendingDecision reports whether line opens one of the pendingDecisions.
func pendingDecision(line string) bool {
	for _, g := range pendingDecisions {
		if strings.HasPrefix(line, "> **Open ("+g+").**") {
			return true
		}
	}
	return false
}

// specHeadings returns the numbered headings of every chapter file of the
// spec, as a map from section number to title with the backquotes removed.
// It checks that each number appears once, in the file of its chapter.
func specHeadings(t *testing.T) map[string]string {
	t.Helper()
	files, err := os.ReadDir(specDir)
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]string{}
	where := map[string]string{}
	for _, f := range files {
		m := chapterFile.FindStringSubmatch(f.Name())
		if m == nil {
			t.Errorf("docs/spec/language/%s: not a chapter file (NN-title.md or x-title.md)", f.Name())
			continue
		}
		chapter := strings.TrimLeft(m[1], "0")
		if len(m[1]) == 1 {
			chapter = strings.ToUpper(m[1])
		}
		data, err := os.ReadFile(filepath.Join(specDir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		inFence := false
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			if strings.HasPrefix(line, "> **Open") && !pendingDecision(line) {
				t.Errorf("docs/spec/language/%s: an open question remains: %s", f.Name(), line)
			}
			h := headingForm.FindStringSubmatch(line)
			if h == nil {
				continue
			}
			number := h[1]
			if first, _, _ := strings.Cut(number, "."); first != chapter {
				t.Errorf("docs/spec/language/%s: section %s belongs in the file of chapter %s", f.Name(), number, first)
			}
			if prev, dup := where[number]; dup {
				t.Errorf("docs/spec/language/%s: section %s is numbered again (first in %s)", f.Name(), number, prev)
			}
			where[number] = f.Name()
			headings[number] = strings.ReplaceAll(strings.TrimSpace(h[2]), "`", "")
		}
	}
	return headings
}

// TestSpecReferences checks that every reference to the Plutonium spec in
// the catalog names a section that exists in docs/spec/language/, under
// the title the reference gives, and that every section number mentioned
// in the sources of the catalog and of the Plutonium error codes
// (compiler/internal/diag, compiler/internal/types2/errors/plutonium.p)
// exists. A renumbering of the spec fails here, so the references are
// updated with it.
func TestSpecReferences(t *testing.T) {
	t.Parallel()
	headings := specHeadings(t)
	if len(headings) < 200 {
		t.Fatalf("found %d numbered headings in docs/spec/language; is the heading form parsed correctly?", len(headings))
	}
	c := readCatalog(t)
	checked := 0
	for _, code := range c.codes() {
		e := c.entries[code]
		if e.Spec == "" {
			continue
		}
		m := pspecForm.FindStringSubmatch(e.Spec)
		if m == nil {
			// The other forms point at the whole of the Plutonium spec. Any
			// mention of a section of it must be in pspec's form.
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
			t.Errorf("%s (%s): docs/spec/language has no section %s (cited as %q)", code, e.Name, section, title)
		case got != title:
			t.Errorf("%s (%s): section %s of docs/spec/language is %q, not %q", code, e.Name, section, got, title)
		}
	}
	if checked == 0 {
		t.Error("no catalog entry cites a section of the Plutonium spec")
	}

	// Sections mentioned in prose, in comments and in the text of
	// explanations and messages.
	sources, err := filepath.Glob(filepath.Join(diagDir, "*.p"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(typerrDir, "plutonium.p"))
	for _, file := range sources {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range sectionMention.FindAllStringSubmatch(line, -1) {
				if _, ok := headings[m[1]]; !ok {
					t.Errorf("%s:%d: docs/spec/language has no section %s", filepath.ToSlash(file), i+1, m[1])
				}
			}
		}
	}
}
