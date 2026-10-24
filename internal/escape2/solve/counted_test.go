package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2"
)

// Views of counted errors (sections 16.6 and 18.3, eir/borrow.go): a
// pointer taken out of a counted error is a view of what holds the error,
// and a counted copy of it (stored, returned, wrapped) is not. Each program
// is rejected at exactly the lines of reject, or accepted.
var countedCases = []struct {
	name, src string
	reject    []string
}{
	{
		// errors.As borrows a child an Unwrap method makes afresh, which
		// the walk releases: reported at the method (diag/counted_error_unwrap).
		"as_fresh_child", `package main

import (
	"pluto/alloc"
	"pluto/errors"
)

type detail struct{ n int }

func (*detail) Error() string { return "detail" }

type root int

func (root) Error() string { return "root" }
func (root) Unwrap() error { return alloc.Synced[detail]() }

func main() {
	var p *detail
	errors.As(root(0), &p)
	println(p.n)
}
`, []string{"main.p:15:"},
	},
	{
		// A child the wrapping error holds lives as long as it does.
		"as_held_child", `package main

import (
	"pluto/alloc"
	"pluto/errors"
)

type detail struct{ n int }

func (*detail) Error() string { return "detail" }

type wrap struct{ err error }

func (w *wrap) Error() string { return "wrap" }
func (w *wrap) Unwrap() error { return w.err }

func main() {
	w := &wrap{err: alloc.Synced[detail]()}
	var p *detail
	errors.As(w, &p)
	println(p.n)
}
`, nil,
	},
	{
		// A pointer taken out of an error a call returns is a view of the
		// statement's reference, released at its end.
		"view_of_temporary", `package main

import "pluto/alloc"

type detail struct{ n int }

func (*detail) Error() string { return "detail" }

func mk() error { return alloc.Synced[detail]() }

var keep *detail

func f() {
	keep = mk().(*detail)
}

func main() { f() }
`, []string{"main.p:14:"},
	},
	{
		// A counted error stored, returned, unwrapped or wrapped is a
		// reference of its own.
		"counted_copies", `package main

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
)

type detail struct{ n int }

func (*detail) Error() string { return "detail" }

type holder struct{ err error }

var kept holder

func keep(err error) { kept.err = err }

func pass(err error) error { return err }

func main() {
	err := fmt.Errorf("outer: %w", error(alloc.Synced[detail]()))
	keep(err)
	println(pass(err) != nil)
	fmt.Println(errors.Unwrap(err))
	var p *detail
	println(errors.As(err, &p), p.n)
}
`, nil,
	},
}

func TestCountedErrorViews(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the library")
	}
	for _, c := range countedCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "main.p")
			if err := os.WriteFile(f, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			err := escape2.Check(loadFile(t, f))
			var lines []string
			if err != nil {
				for _, l := range strings.Split(err.Error(), "\n") {
					if i := strings.Index(l, "main.p:"); i >= 0 && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
						rest := l[i:]
						if j := strings.IndexByte(rest[len("main.p:"):], ':'); j >= 0 {
							lines = append(lines, rest[:len("main.p:")+j+1])
						}
					}
				}
			}
			got := strings.Join(dedupe(lines), " ")
			want := strings.Join(c.reject, " ")
			if got != want {
				t.Errorf("rejected at %q, want %q:\n%v", got, want, err)
			}
		})
	}
}
