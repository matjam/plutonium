package build_test

import (
	"strings"
	"testing"
)

// TestUnsupportedErrors checks that constructs the compiler cannot lower,
// and enum misuse, stop the build with an error rather than producing code.
func TestUnsupportedErrors(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"deferred copy", `package main

func main() {
	var a, b [2]int
	defer copy(a[:], b[:])
}
`, "defer of builtin copy is not supported"},
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
			err := buildError(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}
