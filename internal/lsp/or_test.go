package lsp

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// orSrc is a main package with or statements (language spec 11.27).
const orSrc = `package main

import "pluto/strconv"

func parse(s string) (int, error) {
	n := strconv.Atoi(s) or return
	m := strconv.Atoi(s) or {
		return 0, err
	}
	return n + m, nil
}

func main() {
	parse("1") or panic(err)
}
`

// TestOrCompletionAndHover checks that completion offers or, and only or,
// after the closing parenthesis of a call, and that hovering or explains
// the clause and hovering err in a clause describes the error variable.
func TestOrCompletionAndHover(t *testing.T) {
	root := writeFiles(t, demo)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	complete := func(src, marker string) []string {
		t.Helper()
		c.edit(main, src)
		var list CompletionList
		pos := newText([]byte(src)).position(strings.Index(src, marker) + len(marker))
		c.result("textDocument/completion", posParams(main, pos), &list)
		var labels []string
		for _, it := range list.Items {
			labels = append(labels, it.Label)
		}
		return labels
	}
	withStmt := func(stmt string) string {
		return strings.Replace(orSrc, "\tparse(\"1\") or panic(err)\n", "\tparse(\"1\") or panic(err)\n\t"+stmt+"\n", 1)
	}
	for _, tc := range []struct {
		name, src, marker string
		want, notWant     []string
	}{
		{"after a call", withStmt("x := parse(\"2\") o"), "parse(\"2\") o", []string{"or"}, []string{"os", "parse"}},
		{"after a call, nothing typed", withStmt("parse(\"2\") "), "parse(\"2\") ", []string{"or"}, []string{"parse"}},
		{"not after a parenthesis", withStmt("x := o"), "x := o", nil, []string{"or"}},
		{"after a parenthesized expression", withStmt("x := (1) o"), "(1) o", nil, []string{"or"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := complete(tc.src, tc.marker)
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Errorf("completions %v lack %s", got, w)
				}
			}
			for _, w := range tc.notWant {
				if slices.Contains(got, w) {
					t.Errorf("completions %v include %s", got, w)
				}
			}
		})
	}

	c.edit(main, orSrc)
	hover := func(marker string, n int) string {
		t.Helper()
		var h *Hover
		pos := newText([]byte(orSrc)).position(strings.Index(orSrc, marker) + n)
		c.result("textDocument/hover", posParams(main, pos), &h)
		if h == nil {
			return ""
		}
		return h.Contents.Value
	}
	if got := hover("or return", 1); !strings.Contains(got, "an or clause") || !strings.Contains(got, "`or return` passes the error up") {
		t.Errorf("hover on or = %q", got)
	}
	if got := hover("or {", 0); !strings.Contains(got, "an or clause") {
		t.Errorf("hover on a block clause's or = %q", got)
	}
	if got := hover("return 0, err", 10); !strings.Contains(got, "var err error") {
		t.Errorf("hover on err = %q", got)
	}
	if got := hover("strconv.Atoi(s) or", 3); strings.Contains(got, "an or clause") {
		t.Errorf("hover on the call = %q", got)
	}
}
