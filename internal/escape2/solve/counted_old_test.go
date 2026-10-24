package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2"
)

// errText is err's text, or "accepted".
func errText(err error) string {
	if err == nil {
		return "accepted"
	}
	return err.Error()
}

// fullDiagText is err printed in full, codes included.
func fullDiagText(err error) string {
	var b strings.Builder
	diag.Print(&b, err, false)
	return b.String()
}

// countedGaps are the old analysis's cases escape2 does not decide as it
// does yet, with the reason; the test fails when one is decided so, and the
// gap is to be removed (docs/plans/2026-10-20-escape-redesign.md, known gaps).
var countedGaps = map[string]string{
	// A counted error that reaches the Unwrap result through memory the
	// analysis takes as heap: what is loaded from heap memory is not known
	// to be a counted reference made afresh.
	"fresh unwrap counted field":               "a child loaded from a fresh heap box",
	"fresh unwrap counted map":                 "a child loaded from a map",
	"fresh unwrap counted channel":             "a child received from a channel",
	"fresh unwrap pointer field factory":       "a child loaded through a pointer to a fresh box",
	"fresh unwrap slice pointer field factory": "a child loaded through a slice of pointers to a fresh box",
	"fresh unwrap temporary strong field":      "a child loaded from a fresh heap box",
	"unknown unwrap counted result":            "a child a bodyless function returns",
	// errors.As's one body for every instance: a counted target (E a box
	// type) takes a reference of its own, which the generic store cannot
	// show (strict, not unsound).
	"fresh unwrap counted target": "As[synced[E]]: a counted target",
	// A counted error converted to an interface that does not count is a
	// view of its holder; the holder is not added (eir/borrow.go lendView).
	"borrowed any return": "returning any(e) of a local e",
}

// TestCountedErrorBorrowsOld runs the old analysis's counted-error cases
// (internal/escape/counted_error_test.go) through escape2: each is rejected
// with the code given, or accepted, but for the countedGaps.
func TestCountedErrorBorrowsOld(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the library")
	}
	const prefix = `package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
`
	cases := []struct {
		name, body string
		reject     string
	}{
		{"counted return", `func f() error { e := makeError(); return e }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"borrowed any return", `func f() any { e := makeError(); return any(e) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, "E3001"},
		{"borrowed As return", `func f() *E { e := makeError(); var p *E; errors.As(e, &p); return p }
func main() { _ = f() }`, "E3001"},
		{"borrowed helper return", `func borrow(e error) *E { var p *E; errors.As(e, &p); return p }
func f() *E { e := makeError(); return borrow(e) }
func main() { _ = f() }`, "E3001"},
		{"borrowed indirect As return", `func f() *E { e := makeError(); var p *E; as := errors.As[*E]; as(e, &p); return p }
func main() { _ = f() }`, "E3001"},
		{"counted assertion return", `func f() error { e := makeError(); a := any(e); return a.(error) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"synced assertion return", `func f() synced[E] { e := makeError(); return e.(synced[E]) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"synced assertion return of a conversion", `func f() synced[E] { var e error = alloc.Synced[E](); return e.(synced[E]) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"borrowed caller error", `func f(e error) *E { var p *E; errors.As(e, &p); return p }
func main() { e := makeError(); _ = f(e) }`, ""},
		{"counted As target", `func f() error { e := makeError(); var other error; errors.As(e, &other); return other }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"counted helper field", `type holder struct { err error }
func keep(e error, h *holder) { h.err = e }
func f() error { e := makeError(); var h holder; keep(e, &h); return h.err }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, ""},
		{"fresh unwrap borrowed target", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return makeError() }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap indirect borrowed target", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return makeError() }
func main() { var p *E; as := errors.As[*E]; as(root(0), &p) }`, "E3003"},
		{"fresh unwrap counted target", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return makeError() }
func f() synced[E] { var p synced[E]; errors.As(root(0), &p); return p }
func main() { _ = f() }`, ""},
		{"static unwrap borrowed target", `var sentinel = &E{n:42}
type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return sentinel }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }`, ""},
		{"fresh unwrap custom helper", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { return makeError() }
func as[T any](err error, target *T) bool {
 if e, ok := any(err).(T); ok { *target = e; return true }
 if x, ok := err.(interface{Unwrap() error}); ok { return as(x.Unwrap(), target) }
 return false
}
func main() { var p *E; as(root(0), &p); _ = errors.Is(nil, nil) }`, "E3003"},
		{"fresh unwrap counted field", `type holder struct { err error }
type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { h := alloc.Synced[holder](); h.err = makeError(); return h.err }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap counted map", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { m := make(map[int]error); m[0] = makeError(); return m[0] }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap counted slice", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { a := [1]error{makeError()}; s := a[:]; return s[0] }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap counted channel", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { c := make(chan error, 1); c <- makeError(); return <-c }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap counted dereference", `type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { e := makeError(); p := &e; return *p }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"unknown unwrap counted result", `type root int
func (root) Error() string { return "root" }
//pu:extern child
func child() error
func (root) Unwrap() error { return child() }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap generic factory", `type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func (root) Unwrap() error { return box(alloc.Synced[E]()) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap chained generic factory", `type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func forward[T interface{Error() string}](p T) error { return box(p) }
func (root) Unwrap() error { return forward(alloc.Synced[E]()) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap asserted generic factory", `type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error {
 if s, ok := any(p).(synced[E]); ok { return s }
 return p
}
func (root) Unwrap() error { return box(alloc.Synced[E]()) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"static unwrap generic factory", `var sentinel = &E{n:42}
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func (root) Unwrap() error { return box(sentinel) }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }`, ""},
		{"static unwrap chained generic factory", `var sentinel = &E{n:42}
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func forward[T interface{Error() string}](p T) error { return box(p) }
func (root) Unwrap() error { return forward(sentinel) }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }`, ""},
		{"fresh unwrap struct factory", `type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.p; return h.p }
func (root) Unwrap() error { return box(holder{alloc.Synced[E]()}) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap receiver factory", `type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func (h holder) boxed() error { _ = &h.p; return h.p }
func (root) Unwrap() error { return holder{alloc.Synced[E]()}.boxed() }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap pointer field factory", `type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func box(h *holder) error { _ = &h.p; return h.p }
func (root) Unwrap() error { h := alloc.Synced[holder](); h.p = alloc.Synced[E](); return box(&*h) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap nested field factory", `type inner struct { p synced[E] }
type holder struct { inner inner }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.inner.p; return h.inner.p }
func (root) Unwrap() error { return box(holder{inner{alloc.Synced[E]()}}) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap slice factory", `type root int
func (root) Error() string { return "root" }
func box(p []synced[E]) error { return p[0] }
func (root) Unwrap() error { p := [1]synced[E]{alloc.Synced[E]()}; return box(p[:]) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap slice pointer field factory", `type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func box(p []*holder) error { _ = &p[0].p; return p[0].p }
func (root) Unwrap() error { h := alloc.Synced[holder](); h.p = alloc.Synced[E](); p := [1]*holder{&*h}; return box(p[:]) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"held strong field unwrap borrowed target", `type root struct { p synced[E] }
func (r *root) Error() string { return "root" }
func (r *root) Unwrap() error { return r.p }
func main() { r := alloc.Synced[root](); r.p = alloc.Synced[E](); var e error = r; var p *E; errors.As(e, &p); println(p.n) }`, ""},
		{"fresh unwrap temporary strong field", `type holder struct { p synced[E] }
type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { h := alloc.Synced[holder](); h.p = alloc.Synced[E](); return h.p }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap deeper field factory", `type leaf struct { p synced[E] }
type inner struct { leaf leaf }
type holder struct { inner inner }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.inner.leaf.p; return h.inner.leaf.p }
func (root) Unwrap() error { return box(holder{inner{leaf{alloc.Synced[E]()}}}) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"static unwrap nested generic field", `var sentinel = &E{n:42}
type inner[T interface{Error() string}] struct { p T }
type holder[T interface{Error() string}] struct { inner inner[T] }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](h holder[T]) error { _ = &h.inner.p; return h.inner.p }
func (root) Unwrap() error { return box(holder[*E]{inner[*E]{sentinel}}) }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }`, ""},
		{"fresh unwrap deeper generic field", `type leaf[T interface{Error() string}] struct { p T }
type inner[T interface{Error() string}] struct { leaf leaf[T] }
type holder[T interface{Error() string}] struct { inner inner[T] }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](h holder[T]) error { _ = &h.inner.leaf.p; return h.inner.leaf.p }
func (root) Unwrap() error { return box(holder[synced[E]]{inner[synced[E]]{leaf[synced[E]]{alloc.Synced[E]()}}}) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"fresh unwrap forwarded deeper generic field", `type leaf[T interface{Error() string}] struct { p T }
type inner[T interface{Error() string}] struct { leaf leaf[T] }
type holder[T interface{Error() string}] struct { inner inner[T] }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](h holder[T]) error { _ = &h.inner.leaf.p; return h.inner.leaf.p }
func forward[T interface{Error() string}](h holder[T]) error { return box(h) }
func (root) Unwrap() error { return forward(holder[synced[E]]{inner[synced[E]]{leaf[synced[E]]{alloc.Synced[E]()}}}) }
func main() { var p *E; errors.As(root(0), &p) }`, "E3003"},
		{"static unwrap deeper field", `var sentinel = &E{n:42}
type leaf struct { p *E }
type inner struct { leaf leaf }
type holder struct { inner inner }
type root int
func (root) Error() string { return "root" }
func box(h holder) error { _ = &h.inner.leaf.p; return h.inner.leaf.p }
func (root) Unwrap() error { return box(holder{inner{leaf{sentinel}}}) }
func main() { var p *E; errors.As(root(0), &p); println(p.n) }`, ""},
		{"static unwrap generic ordinary struct", `var sentinel = &E{n:42}
type ordinary struct { p *E }
func (o ordinary) Error() string { return "ordinary" }
type root int
func (root) Error() string { return "root" }
func box[T interface{Error() string}](p T) error { return p }
func (root) Unwrap() error { return box(ordinary{sentinel}) }
func main() { var p ordinary; errors.As(root(0), &p); println(p.p.n) }`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "main.p")
			if err := os.WriteFile(f, []byte(prefix+c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			err := escape2.Check(loadFile(t, f))
			var miss string
			switch {
			case c.reject != "" && (err == nil || !strings.Contains(fullDiagText(err), c.reject)):
				miss = "want " + c.reject + ", got " + errText(err)
			case c.reject == "" && err != nil:
				miss = "want accepted, got " + errText(err)
			}
			gap, isGap := countedGaps[c.name]
			switch {
			case isGap && miss == "":
				t.Errorf("decided as the old analysis does now: remove the gap (%s)", gap)
			case !isGap && miss != "":
				t.Error(miss)
			}
		})
	}
}
