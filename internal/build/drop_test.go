package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestDropUnsupported checks that an owner the compiler cannot drop yet is
// a compile error at the variable, not a value silently leaked.
func TestDropUnsupported(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, src, want string }{
		{"owning interface without methods", `package main

type R owner interface{}

func use(r R) {}

func main() { use(nil) }
`, "cannot drop a main.R here: dropping owning interfaces with no methods is not implemented yet"},
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
