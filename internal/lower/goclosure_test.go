package lower_test

import (
	"strings"
	"testing"
)

// goClosureDecls are declarations the go statement closure tests share.
const goClosureDecls = `package main

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func (c counter) get() int { return c.n }

type task func()

func (t *task) set(f func()) { *t = f }

func run(f func()) { f() }

func runAll(fs ...func()) {}

`

// TestGoFrameClosure checks which func values of a go statement the
// compiler can tell are closure records in the starting frame: those are
// compile errors, and the rest are left to the check when the go statement
// runs.
func TestGoFrameClosure(t *testing.T) {
	const code = `(run "pluto explain E2594"`
	cases := []struct {
		name, body string
		want       string // a fragment of the error, or "" for none
	}{
		{"var literal", "n := 1\nf := func() { n++ }\ngo f()", "cannot start f with go: it holds a function literal that captures n"},
		{"var decl", "n := 1\nvar f = func() { n++ }\ngo f()", "cannot start f with go"},
		{"typed var decl", "n := 1\nvar f func() = func() { n++ }\ngo run(f)", "cannot pass f to a puroutine"},
		{"chain", "var c counter\nf := c.add\ng := f\ngo g()", "cannot start g with go: it holds the method value c.add"},
		{"value receiver", "var c counter\ngo run(func() { _ = c.get() })", "cannot pass a function literal that captures c"},
		{"variadic", "var c counter\ngo runAll(c.add)", "cannot pass the method value c.add"},
		{"parenthesized", "var c counter\ngo run((c.add))", "cannot pass the method value c.add"},
		{"defined in if", "n := 1\nif f := func() { n++ }; n > 0 {\n\tgo run(f)\n}", "cannot pass f"},

		{"no captures", "f := func() {}\ngo f()\ngo run(f)", ""},
		{"function", "go run(run2)", ""},
		{"assigned again", "n := 1\nf := func() {}\nf = func() { n++ }\ngo f()", ""},
		{"address taken", "n := 1\nf := func() { n++ }\np := &f\n*p = func() {}\ngo f()", ""},
		{"method on var", "n := 1\nf := task(func() { n++ })\nf.set(func() {})\ngo run(f)", ""},
		{"range assigns", "n := 1\nf := func() { n++ }\nfor _, f = range []func(){nil} {\n}\ngo run(f)", ""},
		{"multi value", "f, ok := pair()\ngo run(f)\n_ = ok", ""},
		{"var without value", "n := 1\nvar f func()\nf = func() { n++ }\ngo run(f)", ""},
		{"parameter", "start(func() {})", ""},
		{"field", "var s struct{ f func() }\nn := 1\ns.f = func() { n++ }\ngo run(s.f)", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := goClosureDecls +
				"func run2() {}\n\nfunc pair() (func(), bool) { return nil, true }\n\n" +
				"func start(f func()) { go run(f) }\n\n" +
				"func main() {\n\t" + strings.ReplaceAll(c.body, "\n", "\n\t") + "\n}\n"
			got := lowerModeError(t, src, hosted)
			if c.want == "" {
				if got != "" {
					t.Errorf("reported:\n%s", got)
				}
				return
			}
			if !containsText(got, c.want) || !strings.Contains(got, code) {
				t.Errorf("got:\n%s\nwant a report containing %q", got, c.want)
			}
		})
	}
}
