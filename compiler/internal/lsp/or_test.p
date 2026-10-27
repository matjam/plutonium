// Ported from internal/lsp/or_test.go at the go-cutover tag.

package lsp

import (
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
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
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	withStmt := func(stmt string) string {
		return strings.Replace(orSrc, "\tparse(\"1\") or panic(err)\n", "\tparse(\"1\") or panic(err)\n\t"+stmt+"\n", 1)
	}
	for _, tc := range []completionCase{
		{"after a call", withStmt("x := parse(\"2\") o"), "parse(\"2\") o", []string{"or"}, []string{"os", "parse"}},
		{"after a call, nothing typed", withStmt("parse(\"2\") "), "parse(\"2\") ", []string{"or"}, []string{"parse"}},
		{"not after a parenthesis", withStmt("x := o"), "x := o", nil, []string{"or"}},
		{"after a parenthesized expression", withStmt("x := (1) o"), "(1) o", nil, []string{"or"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkLabels(t, c.complete(main, tc.src, tc.marker), tc.want, tc.notWant)
		})
	}

	c.edit(main, orSrc)
	hover := func(marker string, n int) string {
		t.Helper()
		pos := newText(compat.Bytes(orSrc)).position(strings.Index(orSrc, marker) + n)
		h := c.hover(main, pos)
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
