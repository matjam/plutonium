// Ported from internal/build/fieldnames_test.go at the go-cutover tag.

package tests

import (
	"path/filepath"
	"testing"
)

// TestFieldNames builds a program that prints a struct with %+v and %#v
// with field names kept and stripped (--fieldnames=false): without names
// both print the fields as %v does, in Go syntax for %#v.
func TestFieldNames(t *testing.T) {
	t.Parallel()
	const src = `package main

import "pluto/fmt"

type point struct {
	X, Y int
	tag  string
}

func main() {
	fmt.Printf("%v|%+v|%#v\n", point{1, 2, "a"}, point{3, 4, "b"}, point{5, 6, "c"})
}
`
	// The setting is the value of --fieldnames, which is a boolean flag
	// (the build's "on" and "off"); empty leaves the flag out.
	cases := []struct{ setting, want string }{
		{"", `{1 2 a}|{X:3 Y:4 tag:b}|main.point{X:5, Y:6, tag:"c"}` + "\n"},
		{"true", `{1 2 a}|{X:3 Y:4 tag:b}|main.point{X:5, Y:6, tag:"c"}` + "\n"},
		{"false", `{1 2 a}|{3 4 b}|main.point{5, 6, "c"}` + "\n"},
	}
	for _, c := range cases {
		t.Run("fieldnames="+c.setting, func(t *testing.T) {
			t.Parallel()
			var flags []string
			if c.setting != "" {
				flags = append(flags, "--fieldnames="+c.setting)
			}
			exe := mustBuild(t, single(src), append([]string{"-O2"}, flags...)...)
			got, status := runExe(t, exe, true)
			if status != 0 {
				t.Fatalf("exit status %d\n%s", status, got)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	// A value that is not a boolean is refused.
	if out, status := runPluto(t, ".", nil, "build", "--fieldnames=maybe", "-o", filepath.Join(t.TempDir(), "prog"), writeProgram(t, src)); status == 0 {
		t.Errorf("pluto build accepted --fieldnames=maybe:\n%s", out)
	}
}
