package build_test

import (
	"strings"
	"testing"
)

// TestDropUnsupported checks that an owner the compiler cannot drop yet is
// a compile error at the variable, not a value silently leaked.
func TestDropUnsupported(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"owned field", `package main

type B owner struct{ p owned[int] }

func main() {
	var b B
	_ = b
}
`, "main.p:6:2: cannot drop a main.B here: dropping owned, shared and synced references is not implemented yet"},
		{"owning interface", `package main

type R owner interface{ Read() int }

func use(r R) {}

func main() { use(nil) }
`, "cannot drop a main.R here: dropping owning interfaces is not implemented yet"},
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
