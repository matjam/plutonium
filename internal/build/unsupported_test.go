package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestUnsupportedErrors checks that constructs the compiler cannot lower,
// and enum misuse, stop the build with an error rather than producing code.
func TestUnsupportedErrors(t *testing.T) {
	testguard.Parallel(t)
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
			testguard.Parallel(t)
			err := buildError(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}
