package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/testguard"
)

// fullError returns the diagnostics in err as pluto prints them, with
// their explanations.
func fullError(err error) string {
	var b strings.Builder
	diag.Print(&b, err, false)
	return b.String()
}

// TestKernelInitAlloc checks that package initialization that can reach
// an allocation is a compile error in a freestanding build, which has no
// allocator yet (language spec section 15.2), and that static initializers
// are not.
func TestKernelInitAlloc(t *testing.T) {
	testguard.Parallel(t)
	rejected := []struct {
		name, decls string
		want        []string
	}{
		{"make map", "var cache = make(map[string]int)",
			[]string{"main.p:3:13: the initializer of cache allocates, but a freestanding program has no allocator during package initialization",
				"The initializer of cache makes a map (main.p:3).", "var cache map[string]int", "E2586"}},
		{"new", "type T struct{ n int }\n\nvar p = new(T)",
			[]string{"the initializer of p allocates", "The initializer of p allocates (main.p:5)."}},
		{"through a call", "var names = load()\n\nfunc load() []string {\n\treturn grow(nil)\n}\n\nfunc grow(s []string) []string {\n\tm := map[int]bool{1: true}\n\t_ = m\n\treturn s\n}",
			[]string{"the initializer of names allocates", "The initializer of names calls load (main.p:3), which calls grow (main.p:6), which makes a map (main.p:10)."}},
		{"string concatenation", "var a = \"x\"\n\nvar b = a + \"y\"",
			[]string{"the initializer of b allocates"}},
		{"init function", "var m map[int]int\n\nfunc init() {\n\tm = make(map[int]int)\n}",
			[]string{"main.p:6:6: the init function at main.p:5 allocates", "The init function makes a map (main.p:6).", "Do the work of the init function in the program's entry function"}},
		{"library", "import \"pluto/strings\"\n\nvar v = strings.Repeat(\"ab\", 2)",
			[]string{"the initializer of v allocates", "The initializer of v calls strings.Repeat (main.p:5), which allocates."}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			testguard.Parallel(t)
			src := "package main\n\n" + tc.decls + "\n\nfunc main() {}\n"
			_, err := kernelCompile(t, src, 2)
			if err == nil {
				t.Fatalf("compiled; want an error")
			}
			msg := strings.Join(strings.Fields(fullError(err)), " ")
			for _, w := range tc.want {
				if !strings.Contains(msg, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("error does not contain %q:\n%s", w, fullError(err))
				}
			}
		})
	}

	accepted := `package main

import "pluto/errors"

type node struct {
	name string
	next *node
}

type entry struct {
	k string
	v int
}

var (
	ErrBad    = errors.New("bad")
	root      = &node{name: "root"}
	table     = []entry{{"a", 1}, {"b", 2}}
	greeting  = "hello, " + "world"
	answer    = 6 * 7
	asIface   any = entry{"c", 3}
	countdown = [3]int{3, 2, 1}
)

func main() {
	_, _, _, _, _, _, _ = ErrBad, root, table, greeting, answer, asIface, countdown
}
`
	t.Run("static", func(t *testing.T) {
		testguard.Parallel(t)
		if _, err := kernelCompile(t, accepted, 2); err != nil {
			t.Fatalf("static initializers rejected: %v", err)
		}
	})
}
