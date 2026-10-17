package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestErrorConventionErrors checks that errors.New with a message built at
// run time, and asking errors.AsType for something that is not an error,
// are compile errors. (Building strings at run time is allowed since
// strings are counted; errors.New still takes only a constant message,
// since each call site has its own error type, made when compiling.)
func TestErrorConventionErrors(t *testing.T) {
	testguard.Parallel(t)
	const notConstant = "errors.New needs a constant message, since each call site has its own error type, made when compiling; use fmt.Errorf to build a message at run time"
	cases := []struct{ name, src, want string }{
		{"concatenated message", `package main

import "pluto/errors"

func fail(name string) error { return errors.New("open " + name) }

func main() { _ = fail("x") }
`, notConstant},
		{"message from bytes", `package main

import "pluto/errors"

var ErrBad error

func main() {
	b := []byte{'x'}
	ErrBad = errors.New(string(b))
}
`, notConstant},
		{"message from a variable", `package main

import "pluto/errors"

var msg = "bad"

var ErrBad = errors.New(msg)

func main() { _ = ErrBad }
`, notConstant},
		{"AsType of a non-error", `package main

import "pluto/errors"

func main() {
	_, _ = errors.AsType[int](nil)
}
`, "int does not satisfy error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
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

// TestErrorsNewUses checks that errors.New, whose error belongs to a call
// site, can only be called: taking its value or deferring it is a compile
// error. testdata/lib/errorsnew.p covers the calls themselves.
func TestErrorsNewUses(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, body, want string }{
		{"func value", "f := errors.New\n\t_ = f", "errors.New cannot be used as a func value"},
		{"func value argument", "apply(errors.New)", "errors.New cannot be used as a func value"},
		{"deferred", "defer errors.New(\"x\")", "errors.New cannot be deferred"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			src := "package main\n\nimport \"pluto/errors\"\n\nfunc apply(f func(string) error) { _ = f }\n\nfunc main() {\n\t" + c.body + "\n}\n"
			err := buildError(t, src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}
