// Ported from internal/build/unsupported_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestUnsupportedErrors checks that constructs the compiler cannot lower,
// and enum misuse, stop the build with an error rather than producing code.
func TestUnsupportedErrors(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"deferred len", `package main

func main() {
	var a [2]int
	defer len(a)
}
`, "defer of builtin len is not supported"},
		{"non-exhaustive enum switch", `package main

type Color enum { Red; Green; Blue }

func main() {
	c := Color.Green
	switch c {
	case Red, Green:
	}
}
`, "missing case in switch on c (Color): Blue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			errs := buildFails(t, c.src, "--brief")
			if !strings.Contains(errs, c.want) {
				t.Fatalf("got errors %q, want %q", errs, c.want)
			}
		})
	}
}
