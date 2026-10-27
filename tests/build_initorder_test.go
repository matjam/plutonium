// Ported from internal/build/initorder_test.go at the go-cutover tag.

package tests

import "testing"

// TestInitOrder checks the order in which packages are initialized
// (language spec section 15.3): the runtime packages first, then, given
// the remaining packages sorted by import path, the first package whose
// imports are all initialized, step by step. main imports a and c, and a
// imports d, so the order is c, d, a: c is the first ready package in
// path order, d comes before a because a imports it. A depth-first walk
// over main's sorted imports would give d, a, c. Package b imports
// pluto/strings, whose path sorts after e's, so e is ready first and b
// comes last, after pluto/strings.
// Each package's variable initializers run before its init functions.
func TestInitOrder(t *testing.T) {
	t.Parallel()
	pkg := func(name, imports string) string {
		return "package " + name + "\n\n" + imports + "\nvar V = say(\"" + name + " var\")\n\n" +
			"func say(s string) int {\n\tprintln(s)\n\treturn 1\n}\n\n" +
			"func init() { println(\"" + name + " init\") }\n"
	}
	files := map[string]string{
		"pluto.mod": "module example.com/m\n",
		"a/a.p":     pkg("a", "import \"example.com/m/d\"\n\nvar _ = d.V\n"),
		"b/b.p":     pkg("b", "import \"pluto/strings\"\n\nvar _ = strings.Index(\"ab\", \"b\")\n"),
		"c/c.p":     pkg("c", ""),
		"d/d.p":     pkg("d", ""),
		"e/e.p":     pkg("e", ""),
		"main.p": `package main

import (
	"example.com/m/a"
	"example.com/m/b"
	"example.com/m/c"
	"example.com/m/e"
)

func init() { println("main init") }

func main() { println("main", a.V+b.V+c.V+e.V) }
`,
	}
	want := `c var
c init
d var
d init
a var
a init
e var
e init
b var
b init
main init
main 4
`
	for _, opt := range []int{0, 2} {
		out, status := runFiles(t, files, opt)
		if status != 0 || out != want {
			t.Errorf("-O%d: status %d, output:\n%s\nwant:\n%s", opt, status, out, want)
		}
	}
}
