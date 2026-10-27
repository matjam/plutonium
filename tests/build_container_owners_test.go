// Ported from internal/build/container_owners_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestContainerOwnerCopyErrors checks that the container methods that give
// an element out by value do not compile for a container of owners (language
// spec sections 11.11 and 18.5): a copy would be a second owner. The methods
// that move elements (Push, Pop, Remove, Set, Delete) are covered by
// testdata/plutonium/containerowners.p.
func TestContainerOwnerCopyErrors(t *testing.T) {
	t.Parallel()
	const decls = `package main

import (
	"pluto/hash"
	"pluto/vec"
)

type Conn owner struct{ id int }

func main() {
	v := vec.New[Conn]()
	m := hash.NewMap[string, Conn](0)
	v.Push(Conn{1})
	m.Set("a", Conn{1})
`
	cases := []struct{ name, body, want string }{
		{"Vec.At", "_ = v.At(0)", "cannot copy an element of type main.Conn: it is an owner"},
		{"Vec.Last", "_, _ = v.Last()", "cannot copy an element of type main.Conn: it is an owner"},
		{"Vec.Clone", "_ = v.Clone()", "cannot copy an element of type main.Conn: it is an owner"},
		{"Map.Get", "_, _ = m.Get(\"a\")", "cannot copy an element of type main.Conn: it is an owner"},
		{"Map.Clone", "_ = m.Clone()", "cannot copy an element of type main.Conn: it is an owner"},
		{"Append", "c := Conn{2}\n\tv.Append(c)", "cannot pass c to v.Append as a variadic argument: Conn is an owner type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := buildError(t, decls+"\t"+c.body+"\n}\n")
			if err == nil {
				t.Fatalf("compiled; want error %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error:\n%v\nwant %q", err, c.want)
			}
		})
	}
	// Containers of values still copy.
	const ok = `package main

import (
	"pluto/hash"
	"pluto/vec"
)

func main() {
	v := vec.New[string]()
	v.Push("a")
	w := v.Clone()
	m := hash.NewMap[string, string](0)
	m.Set("k", w.At(0)+v.Index(0))
	n := m.Clone()
	x, _ := n.Get("k")
	println(x, w.Len())
}
`
	if err := buildError(t, ok); err != nil {
		t.Errorf("a container of strings: %v", err)
	}
}
