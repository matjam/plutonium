package build_test

import (
	"strings"
	"testing"
)

// TestErrorConventionErrors checks that the ways of building an error
// message that would allocate behind the program's back, or of asking
// errors.AsType for something that is not an error, are compile errors.
func TestErrorConventionErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"concatenated message", `package main

import "pluto/errors"

func fail(name string) error { return errors.New("open " + name) }

func main() { _ = fail("x") }
`, "runtime string concatenation is not permitted in Plutonium"},
		{"message from bytes", `package main

import "pluto/errors"

var ErrBad error

func main() {
	b := []byte{'x'}
	ErrBad = errors.New(string(b))
}
`, "is not permitted in Plutonium (it allocates a copy)"},
		{"AsType of a non-error", `package main

import "pluto/errors"

func main() {
	_, _ = errors.AsType[int](nil)
}
`, "int does not satisfy error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := buildError(t, c.src)
			if err == nil {
				t.Fatalf("no error, want %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

// TestErrorsNewLimit checks that errors.New, which hands out static storage
// meant for package-level sentinels, panics with an explanation when a
// program calls it more often than that storage allows.
func TestErrorsNewLimit(t *testing.T) {
	const src = `package main

import "pluto/errors"

func main() {
	for i := 0; i < 1000; i++ {
		_ = errors.New("made at run time")
	}
}
`
	out, status := compileAndRun(t, src, 0)
	if status != 2 {
		t.Fatalf("exit status %d, want 2; output:\n%s", status, out)
	}
	const want = "panic: errors.New: more than 512 errors; declare errors in package-level variables, or use fmt.Errorf\n"
	if out != want {
		t.Fatalf("output %q, want %q", out, want)
	}
}
