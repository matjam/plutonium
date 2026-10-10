package build_test

import (
	"strings"
	"testing"
)

// TestUnsupportedErrors checks that constructs the compiler cannot lower,
// and enum misuse, stop the build with an error rather than producing code.
func TestUnsupportedErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"promoted method expression", `package main

type In struct{ n int }

func (i In) Get() int { return i.n }

type Out struct{ In }

func main() {
	f := Out.Get
	_ = f
}
`, "method expression Get of a promoted method is not supported yet"},
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
			err := buildError(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}
